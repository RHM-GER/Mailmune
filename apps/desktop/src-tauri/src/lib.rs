use std::sync::{Arc, Mutex, RwLock};

use futures_util::StreamExt;
use serde::Deserialize;
use serde_json::Value;
use tauri::{
    menu::{Menu, MenuItem},
    tray::TrayIconBuilder,
    AppHandle, Emitter, Manager, WindowEvent,
};
use tauri_plugin_autostart::MacosLauncher;
use tauri_plugin_shell::{
    process::{CommandChild, CommandEvent},
    ShellExt,
};

#[derive(Clone)]
struct AgentConnection {
    endpoint: String,
    token: String,
}

#[derive(Default)]
struct AgentRuntime {
    connection: RwLock<Option<AgentConnection>>,
    child: Mutex<Option<CommandChild>>,
    events_started: std::sync::atomic::AtomicBool,
}

#[derive(Deserialize)]
struct AgentHandshake {
    #[serde(rename = "type")]
    kind: String,
    endpoint: String,
    token: String,
}

#[tauri::command]
async fn agent_request(
    runtime: tauri::State<'_, Arc<AgentRuntime>>,
    method: String,
    path: String,
    body: Option<Value>,
) -> Result<Value, String> {
    if !path.starts_with("/v1/")
        || path.contains("..")
        || path.contains("://")
        || path.contains('\\')
    {
        return Err("Ungültiger Agent-Pfad".into());
    }

    let connection = runtime
        .connection
        .read()
        .map_err(|_| "Agent-Status ist nicht verfügbar")?
        .clone()
        .ok_or("Der lokale Agent startet noch")?;

    let client = reqwest::Client::builder()
        .connect_timeout(std::time::Duration::from_secs(5))
        .timeout(std::time::Duration::from_secs(610))
        .build()
        .map_err(|error| error.to_string())?;
    let url = format!("{}{}", connection.endpoint, path);
    let request = match method.as_str() {
        "GET" => client.get(url),
        "POST" => client.post(url),
        _ => return Err("Nur GET und POST sind erlaubt".into()),
    }
    .bearer_auth(connection.token);
    let request = if let Some(value) = body {
        request.json(&value)
    } else {
        request
    };
    let response = request.send().await.map_err(|error| error.to_string())?;
    let status = response.status();
    let payload = response.text().await.map_err(|error| error.to_string())?;
    if !status.is_success() {
        return Err(format!("Agent-Fehler {}: {}", status.as_u16(), payload));
    }
    serde_json::from_str(&payload).map_err(|error| format!("Ungültige Agent-Antwort: {error}"))
}

fn start_agent(app: &AppHandle, runtime: Arc<AgentRuntime>) -> Result<(), Box<dyn std::error::Error>> {
    let data_directory = app.path().app_data_dir()?;
    std::fs::create_dir_all(&data_directory)?;
    let data_directory_string = data_directory.to_string_lossy().into_owned();
    let (mut events, child) = app
        .shell()
        .sidecar("spam-agent")?
        .args(["--data-dir", data_directory_string.as_str()])
        .spawn()?;
    *runtime.child.lock().map_err(|_| "Agent-Prozessstatus ist gesperrt")? = Some(child);

    let app_for_events = app.clone();
    let runtime_for_events = runtime.clone();
    tauri::async_runtime::spawn(async move {
        while let Some(event) = events.recv().await {
            match event {
                CommandEvent::Stdout(bytes) => {
                    if let Ok(handshake) = serde_json::from_slice::<AgentHandshake>(&bytes) {
                        if handshake.kind == "ready"
                            && handshake.endpoint.starts_with("http://127.0.0.1:")
                            && handshake.token.len() >= 32
                        {
                            if let Ok(mut connection) = runtime_for_events.connection.write() {
                                *connection = Some(AgentConnection {
                                    endpoint: handshake.endpoint,
                                    token: handshake.token,
                                });
                            }
                            // Start the SSE pump exactly once; it reconnects
                            // on its own while the agent is available.
                            if !runtime_for_events
                                .events_started
                                .swap(true, std::sync::atomic::Ordering::SeqCst)
                            {
                                tauri::async_runtime::spawn(pump_agent_events(
                                    app_for_events.clone(),
                                    runtime_for_events.clone(),
                                ));
                            }
                        }
                    }
                }
                CommandEvent::Terminated(_) => {
                    if let Ok(mut connection) = runtime_for_events.connection.write() {
                        *connection = None;
                    }
                }
                _ => {}
            }
        }
    });
    Ok(())
}

/// Parses one SSE frame (`event: …` plus `data: …`) into the payload that is
/// forwarded to the webview as `mailmune://agent-event`.
fn parse_sse_frame(frame: &str) -> Option<Value> {
    let mut event_type = String::new();
    let mut data = String::new();
    for line in frame.lines() {
        if let Some(value) = line.strip_prefix("event:") {
            event_type = value.trim().to_string();
        } else if let Some(value) = line.strip_prefix("data:") {
            data.push_str(value.trim());
        }
    }
    if event_type.is_empty() {
        return None;
    }
    let data_value: Value = serde_json::from_str(&data).unwrap_or(Value::Null);
    Some(serde_json::json!({ "type": event_type, "data": data_value }))
}

/// Streams the agent's SSE endpoint once. Returns true when a connection was
/// established (even if it ended later), false when it could not be built.
async fn stream_once(app: &AppHandle, runtime: &Arc<AgentRuntime>, connection: &AgentConnection) -> bool {
    let client = match reqwest::Client::builder()
        .connect_timeout(std::time::Duration::from_secs(5))
        .build()
    {
        Ok(client) => client,
        Err(_) => return false,
    };
    let response = match client
        .get(format!("{}/v1/events", connection.endpoint))
        .bearer_auth(connection.token.clone())
        .send()
        .await
    {
        Ok(response) if response.status().is_success() => response,
        _ => return false,
    };

    let mut stream = response.bytes_stream();
    let mut buffer = String::new();
    while let Some(chunk) = stream.next().await {
        let Ok(bytes) = chunk else {
            return true;
        };
        buffer.push_str(&String::from_utf8_lossy(&bytes));
        while let Some(position) = buffer.find("\n\n") {
            let frame = buffer[..position].to_string();
            buffer = buffer[position + 2..].to_string();
            if let Some(payload) = parse_sse_frame(&frame) {
                let _ = app.emit("mailmune://agent-event", payload);
            }
        }
        // Stop early when the agent vanished while the stream was open.
        if runtime
            .connection
            .read()
            .map(|guard| guard.is_none())
            .unwrap_or(true)
        {
            return true;
        }
    }
    true
}

/// Keeps an SSE subscription to the local agent alive for the whole app
/// lifetime, with bounded exponential backoff between attempts.
async fn pump_agent_events(app: AppHandle, runtime: Arc<AgentRuntime>) {
    let mut backoff = std::time::Duration::from_secs(2);
    loop {
        let connection = runtime
            .connection
            .read()
            .ok()
            .and_then(|guard| guard.clone());
        match connection {
            Some(connection) => {
                if stream_once(&app, &runtime, &connection).await {
                    backoff = std::time::Duration::from_secs(2);
                }
            }
            None => {}
        }
        tauri::async_runtime::sleep(backoff).await;
        backoff = std::cmp::min(backoff * 2, std::time::Duration::from_secs(30));
    }
}

fn stop_agent(runtime: &Arc<AgentRuntime>) {
    if let Ok(mut child) = runtime.child.lock() {
        if let Some(process) = child.take() {
            let _ = process.kill();
        }
    }
}

fn create_tray(app: &AppHandle, runtime: Arc<AgentRuntime>) -> tauri::Result<()> {
    let open = MenuItem::with_id(app, "open", "Öffnen", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "Beenden", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&open, &quit])?;
    let mut tray = TrayIconBuilder::new()
        .tooltip("Mailmune")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_tray_icon_event(|tray, event| {
            if let tauri::tray::TrayIconEvent::Click {
                button: tauri::tray::MouseButton::Left,
                button_state: tauri::tray::MouseButtonState::Up,
                ..
            } = event
            {
                if let Some(window) = tray.app_handle().get_webview_window("main") {
                    let _ = window.show();
                    let _ = window.set_focus();
                }
            }
        })
        .on_menu_event(move |app, event| match event.id.as_ref() {
            "open" => {
                if let Some(window) = app.get_webview_window("main") {
                    let _ = window.show();
                    let _ = window.set_focus();
                }
            }
            "quit" => {
                stop_agent(&runtime);
                app.exit(0);
            }
            _ => {}
        });
    if let Some(icon) = app.default_window_icon().cloned() {
        tray = tray.icon(icon);
    }
    tray.build(app)?;
    Ok(())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let runtime = Arc::new(AgentRuntime::default());
    let runtime_for_setup = runtime.clone();

    tauri::Builder::default()
        .manage(runtime)
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_autostart::init(
            MacosLauncher::LaunchAgent,
            None,
        ))
        .plugin(tauri_plugin_updater::Builder::new().build())
        .setup(move |app| {
            if cfg!(debug_assertions) {
                app.handle().plugin(
                    tauri_plugin_log::Builder::default()
                        .level(log::LevelFilter::Info)
                        .build(),
                )?;
            }
            start_agent(app.handle(), runtime_for_setup.clone())?;
            create_tray(app.handle(), runtime_for_setup.clone())?;
            Ok(())
        })
        .on_window_event(|window, event| {
            if let WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .invoke_handler(tauri::generate_handler![agent_request])
        .run(tauri::generate_context!())
        .expect("Mailmune konnte nicht gestartet werden");
}
