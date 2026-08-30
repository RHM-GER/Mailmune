use std::sync::{Arc, Mutex, RwLock};

use serde::Deserialize;
use serde_json::Value;
use tauri::{
    menu::{Menu, MenuItem},
    tray::TrayIconBuilder,
    AppHandle, Manager, WindowEvent,
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

    tauri::async_runtime::spawn(async move {
        while let Some(event) = events.recv().await {
            match event {
                CommandEvent::Stdout(bytes) => {
                    if let Ok(handshake) = serde_json::from_slice::<AgentHandshake>(&bytes) {
                        if handshake.kind == "ready"
                            && handshake.endpoint.starts_with("http://127.0.0.1:")
                            && handshake.token.len() >= 32
                        {
                            if let Ok(mut connection) = runtime.connection.write() {
                                *connection = Some(AgentConnection {
                                    endpoint: handshake.endpoint,
                                    token: handshake.token,
                                });
                            }
                        }
                    }
                }
                CommandEvent::Terminated(_) => {
                    if let Ok(mut connection) = runtime.connection.write() {
                        *connection = None;
                    }
                }
                _ => {}
            }
        }
    });
    Ok(())
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
