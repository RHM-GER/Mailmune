import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import { Archive, ArchiveRestore, ArrowDown, ArrowUp, ArrowUpDown, Bell, BellDot, Bot, CalendarDays, Check, ChevronDown, ChevronLeft, ChevronRight, ChevronsUpDown, CircleDot, Copy, CornerDownLeft, Eye, EyeOff, Gauge, Globe2, GlobeCheck, GlobeX, Inbox, Info, LayoutDashboard, ListFilter, Mail, MailCheck, MailOpen, Minus, Monitor, PanelLeftClose, Pencil, Plus, RefreshCw, RotateCcw, Search, Settings, ShieldCheck, Square, Tag, Table2, Text, Trash2, X } from "lucide-react"
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip as ChartTooltip, XAxis, YAxis } from "recharts"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { useTheme } from "@/components/theme-provider"
import { agentRequest, cancelScan, compileProfile, deleteAccount, demoDecisions, demoSummary, emptySummary, ensureOllamaRunning, exportTransfer, getBaseline, getEmbeddingStatus, getProfileModel, isTauri, listenAgentEvents, models as listModels, resetLearning, setAccountModel, setProfileModelEnabled, startScan, stats as fetchStats, validateAccountModel } from "@/lib/api"
import type { Account, AgentEvent, DailyStat, Decision, EmbeddingStatus, LearningBaseline, ProfileModel, SafetyMode, ScanEvent, Summary } from "@/lib/api"
import { getCurrentWindow } from "@tauri-apps/api/window"
import { isPermissionGranted, requestPermission, sendNotification } from "@tauri-apps/plugin-notification"

type Page = "dashboard" | "review" | "notifications" | "settings"
type Range = "week" | "month" | "year" | "all" | "custom"
type SortKey = "receivedAt" | "from" | "subject" | "score" | "status" | "category"
type SortDirection = "asc" | "desc" | null

const shortDivider = "relative after:absolute after:right-0 after:top-1/2 after:h-4 after:w-px after:-translate-y-1/2 after:bg-white/10"

function useMediaQuery(query: string) {
  const [matches, setMatches] = useState(() => typeof window !== "undefined" && window.matchMedia(query).matches)
  useEffect(() => {
    const media = window.matchMedia(query)
    const update = () => setMatches(media.matches)
    update()
    media.addEventListener("change", update)
    return () => media.removeEventListener("change", update)
  }, [query])
  return matches
}

function readStoredValue(key: string, legacyKey: string, fallback: string) {
  const current = localStorage.getItem(key)
  if (current !== null) return current
  const legacy = localStorage.getItem(legacyKey)
  if (legacy !== null) {
    localStorage.setItem(key, legacy)
    return legacy
  }
  return fallback
}

function scoreColor(score: number) {
  if (score >= 0.8) return "#a8a8a8"
  const orangeShare = Math.round(Math.min(1, Math.max(0, (0.8 - score) / 0.7)) * 100)
  return `color-mix(in srgb, #a8a8a8 ${100 - orangeShare}%, #ff6b2c ${orangeShare}%)`
}

function spamCategory(decision: Decision) {
  const signals = decision.evidence.map((entry) => `${entry.group} ${entry.code}`).join(" ").toLowerCase()
  if (signals.includes("link")) return "Phishing"
  if (signals.includes("account") || signals.includes("konto")) return "Kontobetrug"
  if (signals.includes("mailing") || signals.includes("newsletter")) return "Newsletter"
  if (signals.includes("pressure") || signals.includes("handlungsdruck")) return "Betrugsversuch"
  if (signals.includes("offer") || signals.includes("angebot") || signals.includes("promo")) return "Werbung"
  return "Verdächtiger Inhalt"
}

// Date helpers for the custom range picker (plain JS, no extra dependency).
const chartDataByPeriod: Record<string, Array<{ label: string; spam: number; inbox: number; falsePositive: number; missed: number }>> = {
  Tag: ["00", "04", "08", "12", "16", "20"].map((label, index) => ({ label, spam: [1, 0, 3, 5, 4, 2][index], inbox: [5, 3, 18, 27, 24, 14][index], falsePositive: [0, 0, 0, 1, 0, 0][index], missed: [0, 0, 0, 0, 1, 0][index] })),
  Woche: ["Mo", "Di", "Mi", "Do", "Fr", "Sa", "So"].map((label, index) => ({ label, spam: [8, 17, 11, 23, 14, 6, 7][index], inbox: [54, 68, 61, 77, 70, 39, 31][index], falsePositive: [0, 1, 0, 1, 0, 0, 0][index], missed: [1, 0, 0, 2, 0, 0, 0][index] })),
  Monat: ["KW 1", "KW 2", "KW 3", "KW 4"].map((label, index) => ({ label, spam: [62, 81, 74, 93][index], inbox: [420, 486, 451, 528][index], falsePositive: [2, 3, 1, 4][index], missed: [3, 1, 2, 0][index] })),
  Jahr: ["Jan", "Mär", "Mai", "Jul", "Sep", "Nov"].map((label, index) => ({ label, spam: [231, 284, 318, 296, 347, 371][index], inbox: [2030, 2180, 2340, 2210, 2470, 2590][index], falsePositive: [9, 11, 8, 13, 10, 12][index], missed: [6, 4, 7, 5, 3, 4][index] })),
  Gesamt: ["2022", "2023", "2024", "2025", "2026"].map((label, index) => ({ label, spam: [1820, 2460, 3110, 3840, 2730][index], inbox: [16800, 20100, 24800, 29100, 22400][index], falsePositive: [78, 92, 108, 126, 81][index], missed: [40, 33, 28, 19, 12][index] })),
}

// Benachrichtigungsarten für den Filter; Labels sind neutral gehalten.
type NotificationKind = "scan" | "review" | "schedule" | "error" | "model"
const notificationKindLabels: Record<NotificationKind | "all", string> = { all: "Alle Arten", scan: "Scans", review: "Prüffälle", schedule: "Wochenprüfung", error: "Fehler", model: "KI-Modell" }

const notifications: NotificationItem[] = [
  { id: "weekly-analysis", kind: "scan", title: "Wochenanalyse abgeschlossen", detail: "438 Nachrichten geprüft, 86 als Spam zugeordnet.", time: "Heute, 16:00", action: false },
  { id: "review-required", kind: "review", title: "12 Fälle benötigen eine Prüfung", detail: "Die Bewertung war für eine automatische Zuordnung nicht sicher genug.", time: "Heute, 15:58", action: true },
  { id: "model-available", kind: "model", title: "Lokales Modell verfügbar", detail: "qwen3:4b-instruct antwortet und kann validiert werden.", time: "Gestern", action: false },
]

// Echte Benachrichtigungen aus dem Agent-Eventstream. Zeitstempel werden erst
// beim Rendern formatiert, damit „Heute/Gestern“ über Neustarts korrekt bleibt.
type AgentNotification = { id: string; kind: NotificationKind; title: string; detail: string; time: number; action: boolean; account?: string }
type NotificationItem = { id: string; kind: NotificationKind; title: string; detail: string; time: string; action: boolean; account?: string }

// OS-Push für wichtige Agent-Ereignisse. Während das Fenster im Fokus ist,
// erscheint kein Push (die Meldung ist bereits sichtbar). Die Berechtigung
// wird beim ersten relevanten Ereignis einmalig erfragt; bei Ablehnung
// bleiben nur die In-App-Benachrichtigungen. Fehler werden still ignoriert –
// Push ist ein Zusatzkanal, niemals kritische Funktionalität.
let osPushAllowed = false
async function sendOsNotification(title: string, body: string) {
  if (!isTauri()) return
  try {
    if (!osPushAllowed) {
      osPushAllowed = (await isPermissionGranted()) || (await requestPermission()) === "granted"
      if (!osPushAllowed) return
    }
    if (await getCurrentWindow().isFocused()) return
    await sendNotification({ title, body })
  } catch {
    // silently ignore
  }
}

function formatNotificationTime(ms: number): string {
  const date = new Date(ms)
  const now = new Date()
  const startOfDay = (value: Date) => new Date(value.getFullYear(), value.getMonth(), value.getDate()).getTime()
  const diffDays = Math.round((startOfDay(now) - startOfDay(date)) / 86_400_000)
  const time = date.toLocaleTimeString("de-DE", { hour: "2-digit", minute: "2-digit" })
  if (diffDays === 0) return `Heute, ${time}`
  if (diffDays === 1) return `Gestern, ${time}`
  return `${date.toLocaleDateString("de-DE", { day: "2-digit", month: "2-digit", year: "numeric" })}, ${time}`
}

type ChartPoint = { label: string; spam: number; inbox: number; falsePositive: number; missed: number }

/**
 * Baut die Chart-Daten aus echten Tagesstatistiken des Agenten. Die
 * Browser-Vorschau ohne Agent nutzt weiterhin die Demo-Daten oben.
 */
function buildRealChartData(stats: DailyStat[], period: string): ChartPoint[] {
  const byDay = new Map<string, DailyStat>()
  for (const stat of stats) byDay.set(stat.day, stat)
  const today = new Date()
  const dayKey = (date: Date) => date.toISOString().slice(0, 10)
  const accumulate = (label: string, days: string[]): ChartPoint => {
    let spam = 0
    let processed = 0
    let rejected = 0
    let missed = 0
    for (const day of days) {
      const stat = byDay.get(day)
      if (!stat) continue
      spam += stat.moved + stat.confirmed
      processed += stat.processed
      rejected += stat.rejected
      missed += stat.missed
    }
    return { label, spam, inbox: processed, falsePositive: rejected, missed }
  }
  if (period === "Tag") return [accumulate("Heute", [dayKey(today)])]
  if (period === "Woche") {
    const labels = ["So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"]
    const out: ChartPoint[] = []
    for (let index = 6; index >= 0; index--) {
      const date = new Date(today.getTime() - index * 86400000)
      out.push(accumulate(labels[date.getUTCDay()], [dayKey(date)]))
    }
    return out
  }
  if (period === "Monat") {
    const out: ChartPoint[] = []
    for (let week = 3; week >= 0; week--) {
      const days: string[] = []
      for (let index = week * 7 + 6; index >= week * 7; index--) {
        days.push(dayKey(new Date(today.getTime() - index * 86400000)))
      }
      out.push(accumulate(`KW ${4 - week}`, days))
    }
    return out
  }
  if (period === "Jahr") {
    const monthNames = ["Jan", "Feb", "Mär", "Apr", "Mai", "Jun", "Jul", "Aug", "Sep", "Okt", "Nov", "Dez"]
    const cutoff = new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth() - 11, 1))
    const byMonth = new Map<string, { spam: number; processed: number; rejected: number; missed: number }>()
    for (const stat of stats) {
      if (new Date(stat.day + "T00:00:00Z") < cutoff) continue
      const key = stat.day.slice(0, 7)
      const bucket = byMonth.get(key) ?? { spam: 0, processed: 0, rejected: 0, missed: 0 }
      bucket.spam += stat.moved + stat.confirmed
      bucket.processed += stat.processed
      bucket.rejected += stat.rejected
      bucket.missed += stat.missed
      byMonth.set(key, bucket)
    }
    return [...byMonth.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([key, bucket]) => ({
      label: monthNames[Number(key.slice(5, 7)) - 1],
      spam: bucket.spam,
      inbox: bucket.processed,
      falsePositive: bucket.rejected,
      missed: bucket.missed,
    }))
  }
  // Gesamt: über den gesamten Zeitraum nach Monaten gruppieren, damit die
  // Verteilung als Linie sichtbar wird statt als einzelner Punkt pro Jahr.
  const allMonthNames = ["Jan", "Feb", "Mär", "Apr", "Mai", "Jun", "Jul", "Aug", "Sep", "Okt", "Nov", "Dez"]
  const byMonthAll = new Map<string, { spam: number; processed: number; rejected: number; missed: number }>()
  for (const stat of stats) {
    const key = stat.day.slice(0, 7)
    const bucket = byMonthAll.get(key) ?? { spam: 0, processed: 0, rejected: 0, missed: 0 }
    bucket.spam += stat.moved + stat.confirmed
    bucket.processed += stat.processed
    bucket.rejected += stat.rejected
    bucket.missed += stat.missed
    byMonthAll.set(key, bucket)
  }
  return [...byMonthAll.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([key, bucket]) => ({
    label: `${allMonthNames[Number(key.slice(5, 7)) - 1]} ${key.slice(2, 4)}`,
    spam: bucket.spam,
    inbox: bucket.processed,
    falsePositive: bucket.rejected,
    missed: bucket.missed,
  }))
}

export default function App() {
  const mainScrollRef = useRef<HTMLDivElement>(null)
  const mainFade = useScrollFade(mainScrollRef)
  const [page, setPage] = useState<Page>("dashboard")
  // Demo-Daten laufen nur in der Browser-Vorschau ohne Agent. In der
  // Desktop-App beginnt die Ansicht leer und füllt sich aus echten Daten.
  const [summary, setSummary] = useState<Summary>(isTauri() ? emptySummary : demoSummary)
  const [decisions, setDecisions] = useState<Decision[]>(isTauri() ? [] : demoDecisions)
  const [accounts, setAccounts] = useState<Account[]>([])
  // Aktives Profil: Tabelle, Dashboard und Einstellungen zeigen strikt nur
  // die Daten dieses Postfachs - Profile werden niemals gemischt. Die Wahl
  // bleibt über Neustarts erhalten; ein neues Postfach wird sofort aktiv.
  const [activeAccountId, setActiveAccountId] = useState<string | null>(() => localStorage.getItem("mailmune.activeAccountId"))
  const activeAccountRef = useRef<string | null>(null)
  const [agentOnline, setAgentOnline] = useState(!isTauri())
  const [dailyStats, setDailyStats] = useState<DailyStat[] | null>(null)
  const [scanNotice, setScanNotice] = useState<{ run: ScanEvent["run"]; candidates?: number; folderMessages?: number } | null>(null)
  const scanNoticeTimer = useRef<number | undefined>(undefined)
  // Echte Benachrichtigungen aus dem Eventstream; lokal persistiert, damit die
  // Seite nach einem Neustart nicht leer ist. Demo-Einträge bleiben der
  // Browser-Vorschau ohne Agent vorbehalten.
  const [agentNotifications, setAgentNotifications] = useState<AgentNotification[]>(() => {
    if (!isTauri()) return []
    try { return (JSON.parse(localStorage.getItem("mailmune.notifications") ?? "[]") as AgentNotification[]).slice(0, 50) } catch { return [] }
  })
  useEffect(() => { if (isTauri()) localStorage.setItem("mailmune.notifications", JSON.stringify(agentNotifications)) }, [agentNotifications])
  const pushNotification = (item: AgentNotification) => {
    setAgentNotifications((current) => [item, ...current.filter((entry) => entry.id !== item.id)].slice(0, 50))
    // Reine Scan-Informationen ohne Handlungsbedarf bleiben In-App;
    // alles Wichtige (Verdachtsfälle, Fehler, Wochenprüfung, Modell)
    // geht zusätzlich als OS-Push raus.
    if (item.action || item.kind !== "scan") {
      const label = item.account ? accountsRef.current.find((entry) => entry.id === item.account)?.name ?? item.account : undefined
      void sendOsNotification(label ? `${item.title} · ${label}` : item.title, item.detail)
    }
  }
  // Herkunft von Event-Toasts/-Benachrichtigungen: Account-ID, wenn sie NICHT
  // dem gerade aktiven Profil entspricht – sonst wäre die Pill nur Rauschen.
  const accountsRef = useRef<Account[]>([])
  useEffect(() => { accountsRef.current = accounts }, [accounts])
  const originAccount = (id?: string) => (id && id !== activeAccountRef.current ? id : undefined)
  const originLabel = (id?: string) => {
    const origin = originAccount(id)
    if (!origin) return undefined
    return accountsRef.current.find((item) => item.id === origin)?.name ?? origin
  }
  // Das Archiv liegt in der App, damit der Nav-Punkt verschwindet, sobald
  // alle Benachrichtigungen archiviert sind.
  const [notificationArchive, setNotificationArchive] = useState<Record<string, number>>(() => {
    try {
      const stored = JSON.parse(readStoredValue("mailmune.notificationArchive", "spamalytic.notificationArchive", "{}")) as Record<string, number>
      const cutoff = Date.now() - 180 * 86_400_000
      return Object.fromEntries(Object.entries(stored).filter(([, archivedAt]) => archivedAt >= cutoff).slice(-500))
    } catch { return {} }
  })
  useEffect(() => { localStorage.setItem("mailmune.notificationArchive", JSON.stringify(notificationArchive)) }, [notificationArchive])
  const notificationItems: NotificationItem[] = isTauri() ? agentNotifications.map(({ time, ...rest }) => ({ ...rest, time: formatNotificationTime(time) })) : notifications
  const hasOpenNotifications = notificationItems.some((item) => !notificationArchive[item.id])
  const [compactNav, setCompactNav] = useState(false)
  const narrowApp = useMediaQuery("(max-width: 890px)")
  const effectiveCompactNav = narrowApp || compactNav
  // Effektives aktives Konto: fällt auf das erste zurück, wenn die Wahl fehlt
  // oder das Konto gelöscht wurde.
  const activeAccount = accounts.find((item) => item.id === activeAccountId) ?? accounts[0] ?? null
  const activeId = activeAccount?.id ?? null
  useEffect(() => {
    activeAccountRef.current = activeId
    if (activeId) localStorage.setItem("mailmune.activeAccountId", activeId)
  }, [activeId])

  const refresh = async () => {
    if (!isTauri()) return
    const accountId = activeAccountRef.current
    const query = accountId ? `accountId=${encodeURIComponent(accountId)}` : ""
    try {
      const [nextSummary, nextDecisions, nextAccounts, nextStats] = await Promise.all([
        agentRequest<Summary>("GET", `/v1/summary${query ? `?${query}` : ""}`),
        agentRequest<Decision[]>("GET", `/v1/decisions?limit=250${query ? `&${query}` : ""}`),
        agentRequest<Account[]>("GET", "/v1/accounts"),
        fetchStats(3660, accountId).catch(() => null),
      ])
      setSummary(nextSummary)
      setDecisions(nextDecisions ?? [])
      setAccounts(nextAccounts ?? [])
      setDailyStats(nextStats)
      setAgentOnline(true)
    } catch {
      setAgentOnline(false)
    }
  }

  useEffect(() => {
    if (!isTauri()) return
    const initial = window.setTimeout(() => void refresh(), 0)
    const retry = window.setTimeout(() => void refresh(), 1200)
    const poll = window.setInterval(() => void refresh(), 15000)
    const handleEvent = (event: AgentEvent) => {
      if (event.type === "scan.started" || event.type === "scan.progress") {
        const data = event.data as ScanEvent
        window.clearTimeout(scanNoticeTimer.current)
        setScanNotice({ run: data.run, folderMessages: data.folderMessages })
      } else if (event.type === "scan.finished") {
        const data = event.data as ScanEvent
        setScanNotice({ run: data.run, candidates: data.candidates, folderMessages: data.folderMessages })
        window.clearTimeout(scanNoticeTimer.current)
        scanNoticeTimer.current = window.setTimeout(() => setScanNotice(null), 8000)
        if (data.run.status === "completed") {
          // Routine-Scans ohne Fund erzeugen KEINE Benachrichtigung – nur wenn
          // tatsächlich etwas als Verdachtsfall markiert wurde (oder ein Lauf
          // fehlschlägt) entsteht ein Eintrag.
          if ((data.candidates ?? 0) > 0) {
            pushNotification({ id: `scan-${data.run.id}`, kind: "scan", title: "Prüfung abgeschlossen", detail: `${data.candidates ?? 0} Verdachtsfälle · ${data.run.processed} Nachrichten geprüft · nichts gelöscht`, time: Date.now(), action: true, account: originAccount(data.run.accountId) })
          }
        } else if (data.run.status === "failed") {
          pushNotification({ id: `scan-${data.run.id}`, kind: "error", title: "Prüfung fehlgeschlagen", detail: data.run.error || "Der Lauf wurde nicht abgeschlossen.", time: Date.now(), action: false, account: originAccount(data.run.accountId) })
        }
      } else if (event.type === "schedule.deep_scan") {
        const data = event.data as { accountId?: string }
        pushNotification({ id: `deep-${Date.now()}`, kind: "schedule", title: "Wochenprüfung gestartet", detail: "Alle Mails seit der letzten Wochenprüfung werden erneut mit KI geprüft.", time: Date.now(), action: false, account: originAccount(data.accountId) })
      } else if (event.type === "schedule.error") {
        const data = event.data as { accountId?: string; error?: string }
        pushNotification({ id: `schedule-error-${data.accountId ?? "unknown"}`, kind: "error", title: "Geplante Prüfung fehlgeschlagen", detail: data.error || "Der Agent konnte das Postfach nicht erreichen.", time: Date.now(), action: false, account: originAccount(data.accountId) })
      } else if (event.type === "model.unavailable") {
        // KI konfiguriert, aber Ollama antwortet nicht: sichtbare Warnung,
        // dass ohne laufenden Ollama-Dienst keine KI-Filterung stattfindet.
        // Die ID pro Konto ersetzt frühere Warnungen statt sie zu stapeln.
        const data = event.data as { accountId?: string; model?: string; error?: string }
        pushNotification({ id: `model-unavailable-${data.accountId ?? "unknown"}`, kind: "model", title: "KI-Filterung ausgefallen – Ollama starten", detail: `Das lokale Modell (${data.model ?? "unbekannt"}) ist nicht erreichbar: ${data.error || "Ollama läuft nicht"}. Bitte Ollama starten, sonst prüft nur der Regelfilter.`, time: Date.now(), action: false, account: originAccount(data.accountId) })
      } else if (event.type === "model.error") {
        // Einzelfehler der KI (Timeout, Kontextlänge, Modell-404 …): echte
        // Ursache zeigen, NICHT behaupten, Ollama liefe nicht.
        const data = event.data as { accountId?: string; model?: string; error?: string; summary?: string }
        pushNotification({ id: `model-error-${data.accountId ?? "unknown"}`, kind: "error", title: "KI-Prüfung teilweise fehlgeschlagen", detail: `${data.summary ?? "Einzelne KI-Consults sind fehlgeschlagen"} (${data.model ?? "Modell"}): ${data.error ?? "unbekannter Fehler"}`, time: Date.now(), action: false, account: originAccount(data.accountId) })
      } else if (event.type === "profile.compiled") {
        // Automatische oder manuelle Rekompilierung des Profilmodells melden,
        // damit klar ist, ab wann Indikatoren/Prompt frisch sind.
        const data = event.data as { reason?: string; accountId?: string }
        showToast(data.reason === "auto"
          ? "KI-Profil automatisch neu kompiliert – dein geänderter Profiltext wirkt jetzt."
          : "KI-Profil neu kompiliert.", "info", originLabel(data.accountId))
        void refresh()
      }
      void refresh()
    }
    let stopEvents: (() => void) | undefined
    let disposed = false
    void listenAgentEvents(handleEvent).then((stop) => {
      if (disposed) stop()
      else stopEvents = stop
    })
    return () => {
      disposed = true
      window.clearTimeout(initial)
      window.clearTimeout(retry)
      window.clearTimeout(scanNoticeTimer.current)
      window.clearInterval(poll)
      stopEvents?.()
    }
  }, [])

  // Ollama-Start mit der App: ist die KI eines Profils eingeschaltet, wird
  // Ollama einmalig automatisch gestartet, damit die KI-Filterung sofort
  // einsatzbereit ist statt auf den manuellen Start zu warten.
  const ollamaKicked = useRef(false)
  useEffect(() => {
    if (!isTauri() || ollamaKicked.current) return
    const active = accounts.find((item) => item.id === activeId) ?? accounts[0]
    if (active?.aiEnabled) {
      ollamaKicked.current = true
      void ensureOllamaRunning()
    }
  }, [accounts, activeId])

  // Profilwechsel: alle Datenansichten sofort für das neue Postfach laden.
  useEffect(() => { if (isTauri()) void refresh() }, [activeId])

  // Offene Prüffälle als echte, nachgeführte Benachrichtigung: ersetzt sich
  // selbst, sobald sich die Anzahl ändert, und verschwindet bei null nicht
  // spurlos – sie bleibt als erledigter Eintrag stehen.
  const pushedPendingRef = useRef(0)
  useEffect(() => {
    if (!isTauri()) return
    if (summary.pending <= 0) {
      pushedPendingRef.current = 0
      setAgentNotifications((current) => current.filter((entry) => entry.id !== "review-required"))
      return
    }
    setAgentNotifications((current) => {
      const title = `${summary.pending} Fälle benötigen eine Prüfung`
      const existing = current.find((entry) => entry.id === "review-required")
      if (existing && existing.title === title) return current
      return [{ id: "review-required", kind: "review" as const, title, detail: "Die Bewertung war für eine automatische Zuordnung nicht sicher genug.", time: Date.now(), action: true }, ...current.filter((entry) => entry.id !== "review-required")].slice(0, 50)
    })
    // OS-Push nur, wenn sich die Anzahl wirklich geändert hat (Ref statt
    // Zustandsvergleich, damit der Effect idempotent bleibt).
    if (summary.pending !== pushedPendingRef.current) {
      pushedPendingRef.current = summary.pending
      // KEIN OS-Push hier: Während des Erstscans steigt die Zahl in kurzen
      // Abständen und würde Push-Spam erzeugen. Der einmalige Push kommt mit
      // scan.finished (nur bei Funden); in-app bleibt die Nachverfolgung.
    }
  }, [summary.pending])

  return (
    <TooltipProvider>
      <div className="relative flex h-screen min-h-[620px] overflow-hidden bg-[#171717] text-white">
        <WindowControls />
        <Sidebar page={page} onPage={setPage} compact={effectiveCompactNav} compactLocked={narrowApp} onCompact={() => setCompactNav((value) => !value)} pending={summary.pending} accounts={accounts} activeAccountId={activeId} onSelectAccount={setActiveAccountId} refresh={refresh} notificationDot={hasOpenNotifications} />
        <main className="relative min-w-0 flex-1 overflow-hidden [transform:translateZ(0)]">
          {/* Rahmenloses Fenster: dieser transparente Bereich oben ersetzt die
              native Titelleiste zum Ziehen; Doppelklick maximiert. Er liegt im
              leeren pt-12-Rand der Seiten und verdeckt keine Inhalte. */}
          <div data-tauri-drag-region className="absolute inset-x-0 top-0 z-30 h-9" />
          <div ref={mainScrollRef} className="h-full overflow-y-auto">
          {page === "settings" && <Header page={page} />}
          <div className={`mx-auto w-full max-w-[1500px] px-14 max-[639px]:px-7 ${page === "review" ? "h-screen overflow-hidden pb-0 pt-12" : page === "notifications" ? "pb-10 pt-12" : page === "settings" ? "h-[calc(100vh-100px)] overflow-hidden pb-0 pt-12" : "pb-10 pt-12"}`}>
            {page === "dashboard" && <Dashboard summary={summary} onReview={() => setPage("review")} scrollRef={mainScrollRef} agentOnline={agentOnline} dailyStats={dailyStats} notifications={notificationItems.slice(0, 3)} onNotifications={() => setPage("notifications")} />}
            {page === "review" && <ReviewPage decisions={decisions} refresh={refresh} agentOnline={agentOnline} onCheckMail={() => { const id = activeAccountRef.current; if (id) void startScan(id, false).catch(() => {}) }} />}
            {page === "notifications" && <Notifications scrollRef={mainScrollRef} items={notificationItems} archive={notificationArchive} onArchiveChange={setNotificationArchive} onReview={isTauri() ? () => setPage("review") : undefined} activeAccountId={activeId} accountName={(id) => accounts.find((item) => item.id === id)?.name ?? id} />}
            {page === "settings" && <SettingsPage accounts={accounts} refresh={refresh} activeAccountId={activeId} />}
          </div>
          </div>
          {page !== "review" && page !== "settings" && <ScrollFade strength={mainFade} targetRef={mainScrollRef} />}
        </main>
        {scanNotice && <ScanToast notice={scanNotice} />}
        <ToastHost />
      </div>
    </TooltipProvider>
  )
}

// formatDuration renders a rough remaining-time estimate in German.
function formatDuration(ms: number): string {
  const totalSeconds = Math.max(0, Math.round(ms / 1000))
  if (totalSeconds < 60) return `${totalSeconds} Sek`
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  if (minutes < 60) return seconds > 0 ? `${minutes} Min ${seconds} Sek` : `${minutes} Min`
  const hours = Math.floor(minutes / 60)
  const restMinutes = minutes % 60
  return restMinutes > 0 ? `${hours} Std ${restMinutes} Min` : `${hours} Std`
}

// Minimales Toast-System: Fehler- und Statusmeldungen gehören als Toast
// angezeigt, nicht als loser Text unter irgendwelchen Karten. showToast ist
// modulweit verfügbar; der ToastHost hängt einmal im App-Root.
type ToastItem = { id: number; text: string; tone: "error" | "info"; account?: string; sticky?: boolean }
let toastListeners: Array<(toast: ToastItem) => void> = []
let toastDismissers: Array<(id: number) => void> = []
let toastCounter = 0
function showToast(text: string, tone: "error" | "info" = "info", account?: string, sticky = false): number {
  const toast = { id: ++toastCounter, text, tone, account, sticky }
  for (const listener of toastListeners) listener(toast)
  return toast.id
}

// Entfernt einen Toast vorzeitig (z. B. den Sticky-„läuft“-Toast, sobald die
// Operation abgeschlossen ist).
function dismissToast(id: number) {
  for (const dismiss of toastDismissers) dismiss(id)
}

function ToastHost() {
  const [items, setItems] = useState<ToastItem[]>([])
  const [minimized, setMinimized] = useState<Record<number, boolean>>({})
  const timers = useRef<Record<number, number>>({})
  const scheduleDismiss = (id: number) => {
    window.clearTimeout(timers.current[id])
    timers.current[id] = window.setTimeout(() => {
      setItems((current) => current.filter((item) => item.id !== id))
      delete timers.current[id]
    }, 6000)
  }
  useEffect(() => {
    const listener = (toast: ToastItem) => {
      setItems((current) => [...current.slice(-3), toast])
      if (!toast.sticky) scheduleDismiss(toast.id)
    }
    const dismisser = (id: number) => {
      window.clearTimeout(timers.current[id])
      delete timers.current[id]
      setItems((current) => current.filter((item) => item.id !== id))
      setMinimized((current) => {
        if (!current[id]) return current
        const next = { ...current }
        delete next[id]
        return next
      })
    }
    toastListeners.push(listener)
    toastDismissers.push(dismisser)
    return () => {
      toastListeners = toastListeners.filter((item) => item !== listener)
      toastDismissers = toastDismissers.filter((item) => item !== dismisser)
      Object.values(timers.current).forEach((timer) => window.clearTimeout(timer))
    }
  }, [])
  // Minimieren stoppt den Auto-Dismiss (Toast bleibt erhalten), Wiederherstellen
  // startet ihn neu – lange sichtbare Meldungen liegen so nie im Weg.
  const minimize = (id: number) => {
    window.clearTimeout(timers.current[id])
    delete timers.current[id]
    setMinimized((current) => ({ ...current, [id]: true }))
  }
  const restore = (id: number) => {
    setMinimized((current) => ({ ...current, [id]: false }))
    scheduleDismiss(id)
  }
  const dismiss = (id: number) => {
    window.clearTimeout(timers.current[id])
    delete timers.current[id]
    setItems((current) => current.filter((item) => item.id !== id))
  }
  const visible = items.filter((item) => !minimized[item.id])
  const hidden = items.filter((item) => minimized[item.id])
  if (items.length === 0) return null
  return <div className="pointer-events-none fixed bottom-6 right-6 z-[70] flex w-[380px] max-w-[calc(100vw-3rem)] flex-col items-end gap-2">
    {visible.map((toast) => <div key={toast.id} role="status" className={`toast-enter pointer-events-auto w-full rounded-md border px-3.5 py-3 text-xs leading-5 shadow-[0_20px_40px_rgba(0,0,0,.5)] ${toast.tone === "error" ? "border-[#e07a5f]/30 bg-[#2a201d] text-[#e8b4a4]" : "border-white/10 bg-[#242424] text-[#bbb]"}`}>
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          {toast.account && <span className="mb-1 inline-flex rounded-full border border-white/10 bg-white/[0.06] px-2 py-0.5 text-[10px] text-[#aaa]">{toast.account}</span>}
          {toast.text}
        </div>
        <div className="flex shrink-0 gap-0.5">
          <button type="button" aria-label="Toast minimieren" onClick={() => minimize(toast.id)} className="rounded p-0.5 text-[#777] transition-colors hover:bg-white/[0.06] hover:text-white"><Minus className="size-3.5" /></button>
          <button type="button" aria-label="Toast schließen" onClick={() => dismiss(toast.id)} className="rounded p-0.5 text-[#777] transition-colors hover:bg-white/[0.06] hover:text-white"><X className="size-3.5" /></button>
        </div>
      </div>
    </div>)}
    {hidden.length > 0 && <button type="button" onClick={() => hidden.forEach((item) => restore(item.id))} className="pointer-events-auto flex items-center gap-2 rounded-full border border-white/10 bg-[#242424] px-3 py-1.5 text-[11px] text-[#aaa] shadow-[0_20px_40px_rgba(0,0,0,.5)] transition-colors hover:bg-[#2b2b2b] hover:text-white"><BellDot className="size-3.5" />{hidden.length} minimiert – anzeigen</button>}
  </div>
}

function ScanToast({ notice }: { notice: { run: ScanEvent["run"]; candidates?: number; folderMessages?: number } }) {
  const [collapsed, setCollapsed] = useState(false)
  const finished = notice.run.status !== "running"
  // Tick once per second while running so the remaining-time estimate counts
  // down live instead of only updating on each progress event.
  const [, setTick] = useState(0)
  useEffect(() => {
    if (finished) return
    const id = window.setInterval(() => setTick((value) => value + 1), 1000)
    return () => window.clearInterval(id)
  }, [finished, notice.run.id])
  // Info/laufend ist bewusst neutral (Hellgrau im Dark Theme); auffällige
  // Farbtöne bleiben Fehler/Warnung/Erfolg vorbehalten.
  const indicator = notice.run.status === "failed" || notice.run.status === "interrupted" ? "#e5484d" : notice.run.status === "cancelled" ? "#f5a524" : finished ? "#46a758" : "#d6d6d6"
  const title = finished
    ? notice.run.status === "completed" ? "Prüfung abgeschlossen"
      : notice.run.status === "cancelled" ? "Prüfung abgebrochen"
      : "Prüfung fehlgeschlagen"
    : "Postfach wird geprüft …"
  // Rough ETA: apply the average rate so far (processed / elapsed) to the
  // remaining messages, so the user knows whether to wait minutes or hours.
  let eta: string | null = null
  if (!finished && notice.run.estimatedTotal > notice.run.processed && notice.run.processed > 0 && notice.run.startedAt) {
    const elapsedMs = Date.now() - new Date(notice.run.startedAt).getTime()
    if (elapsedMs > 0) {
      const remaining = notice.run.estimatedTotal - notice.run.processed
      eta = formatDuration((remaining * elapsedMs) / notice.run.processed)
    }
  }
  const detail = finished
    ? notice.run.status === "completed"
      ? `${notice.run.processed} Nachrichten geprüft · ${notice.candidates ?? 0} Verdachtsfälle · nichts verschoben`
      : notice.run.error || "Der Lauf wurde nicht abgeschlossen."
    : (notice.folderMessages ?? 0) > 0 || notice.run.estimatedTotal > 0
      ? `${notice.run.processed} von ${notice.run.estimatedTotal || notice.folderMessages} Nachrichten gelesen${eta ? ` · verbleibend ~${eta}` : ""}`
      : `${notice.run.processed} Nachrichten gelesen`
  if (collapsed) {
    return <div className="pointer-events-none fixed bottom-6 right-6 z-50">
      <button type="button" onClick={() => setCollapsed(false)} aria-label="Prüffortschritt anzeigen" className="pointer-events-auto flex items-center gap-2 rounded-full border border-white/10 bg-[#232323] px-3 py-1.5 text-[11px] text-[#aaa] shadow-xl transition-colors hover:bg-[#2b2b2b] hover:text-white">
        <span className={`size-2 rounded-full ${finished ? "" : "toast-pulse"}`} style={{ background: indicator }} />
        {finished ? title : `${notice.run.processed}/${notice.run.estimatedTotal || notice.folderMessages || "…"}`}
      </button>
    </div>
  }
  return <div className="pointer-events-none fixed bottom-6 right-6 z-50">
    <div role="status" className="pointer-events-auto flex w-80 max-w-[calc(100vw-3rem)] items-start gap-3 rounded-xl border border-white/10 bg-[#232323] p-4 shadow-xl">
      <span className={`mt-1.5 size-2.5 shrink-0 rounded-full ${finished ? "" : "toast-pulse"}`} style={{ background: indicator }} />
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium">{title}</p>
        <p className="mt-1 text-xs leading-5 text-[#888]">{detail}</p>
        {!finished && <div className="mt-2 flex justify-end"><button type="button" onClick={() => { void cancelScan(notice.run.accountId).catch((cause) => showToast(cause instanceof Error ? cause.message : String(cause), "error")) }} className="rounded-md border border-white/10 bg-white/[0.05] px-2.5 py-1 text-[11px] text-[#aaa] transition-colors hover:bg-white/[0.08] hover:text-white">Abbrechen</button></div>}
      </div>
      {!finished && <button type="button" onClick={() => setCollapsed(true)} aria-label="Prüffortschritt minimieren" className="shrink-0 rounded-md p-1 text-[#777] transition-colors hover:bg-white/[0.05] hover:text-white"><Minus className="size-3.5" /></button>}
    </div>
  </div>
}

function Sidebar({ page, onPage, compact, compactLocked, onCompact, pending, accounts, activeAccountId, onSelectAccount, refresh, notificationDot }: { page: Page; onPage: (page: Page) => void; compact: boolean; compactLocked: boolean; onCompact: () => void; pending: number; accounts: Account[]; activeAccountId: string | null; onSelectAccount: (id: string) => void; refresh: () => void; notificationDot: boolean }) {
  const primary = [{ id: "dashboard" as const, label: "Übersicht", icon: LayoutDashboard }, { id: "review" as const, label: "Zuordnung", icon: Table2 }]
  const secondary = [{ id: "notifications" as const, label: "Benachrichtigungen", icon: Bell }, { id: "settings" as const, label: "Einstellungen", icon: Settings }]
  return (
    <aside className={`shrink-0 bg-[#171717] transition-[width,padding] duration-200 ${compact ? "w-[84px] p-3" : "w-[320px] p-4"}`}>
      <div className={`flex h-full flex-col rounded-[15px] border border-white/10 bg-[#1d1d1d] transition-[padding] duration-200 ${compact ? "p-2" : "p-4"}`}>
        <div className={`flex items-center ${compact ? "h-14 justify-center" : "relative h-[72px] justify-between px-4 pb-6 pt-4 after:absolute after:inset-x-4 after:bottom-0 after:h-px after:bg-white/10"}`}>
          {compact ? compactLocked ? <div className="flex size-10 items-center justify-center"><img src="/mailmune-logo.svg" alt="" className="h-4 w-5" /></div> : <button className="group relative flex size-10 items-center justify-center rounded-md border border-transparent outline-none transition-colors hover:border-white/10 hover:bg-white/[0.05]" onClick={onCompact} aria-label="Navigation ausklappen"><img src="/mailmune-logo.svg" alt="" className="h-4 w-5 transition-opacity group-hover:opacity-0" /><ChevronRight className="absolute size-4 text-[#a8a8a8] opacity-0 transition-opacity group-hover:opacity-100" /></button> : <div className="flex shrink-0 items-center gap-2"><img src="/mailmune-logo.svg" alt="" className="h-4 w-5 shrink-0" /><span className="whitespace-nowrap text-[13px] font-medium">Mailmune <span className="relative -top-1 text-[8px] text-[#888]">by RHMedia</span></span></div>}
          {!compact && <button className="text-[#999] transition-colors hover:text-white" onClick={onCompact} aria-label="Navigation einklappen"><PanelLeftClose className="size-4" /></button>}
        </div>
        <nav className={`${compact ? "mt-4" : "mt-6"} flex flex-1 flex-col gap-0`}>
          {primary.map((item) => <NavItem key={item.id} {...item} active={page === item.id} compact={compact} onClick={() => onPage(item.id)} badge={item.id === "review" && pending ? pending : undefined} />)}
        </nav>
        <nav className="flex flex-col gap-0">
          {secondary.map((item) => <NavItem key={item.id} {...item} active={page === item.id} compact={compact} onClick={() => onPage(item.id)} dot={item.id === "notifications" && notificationDot} />)}
        </nav>
        <AccountSwitcher compact={compact} accounts={accounts} activeAccountId={activeAccountId} onSelectAccount={onSelectAccount} refresh={refresh} />
      </div>
    </aside>
  )
}

function NavItem({ label, icon: Icon, active, compact, onClick, badge, dot }: { id: string; label: string; icon: typeof Bell; active: boolean; compact: boolean; onClick: () => void; badge?: number; dot?: boolean }) {
  const button = <button onClick={onClick} aria-label={compact ? label : undefined} className={`flex h-11 w-full items-center rounded-md border text-sm outline-none transition-colors focus-visible:border-white/20 ${compact ? "justify-center px-0" : "gap-3 px-3.5"} ${active ? "border-white/10 bg-white/[0.06] text-white" : "border-transparent text-[#a8a8a8] hover:border-white/10 hover:bg-white/[0.04] hover:text-white"}`}>
    <span className="relative shrink-0"><Icon className="size-4" />{dot && <span className="absolute -right-0.5 -top-0.5 size-1.5 rounded-full bg-[#ff6b2c] ring-2 ring-[#1d1d1d]" />}</span>{!compact && <><span className="truncate">{label}</span>{badge ? <span className="ml-auto flex min-w-[30px] items-center justify-center rounded-[4px] bg-white px-2 py-0.5 text-[11px] font-medium text-[#666]">{badge}</span> : null}</>}
  </button>
  if (!compact) return button
  return <Tooltip><TooltipTrigger render={button} /><TooltipContent side="right" sideOffset={10}>{label}</TooltipContent></Tooltip>
}

function AccountSwitcher({ compact, accounts, activeAccountId, onSelectAccount, refresh }: { compact: boolean; accounts: Account[]; activeAccountId: string | null; onSelectAccount: (id: string) => void; refresh: () => void }) {
  const [addOpen, setAddOpen] = useState(false)
  const active = accounts.find((item) => item.id === activeAccountId) ?? accounts[0] ?? null
  const initials = (name: string) => name.trim().slice(0, 2).toUpperCase() || "?"
  return <div className="mt-4">
    <DropdownMenu>
      <DropdownMenuTrigger render={<button aria-label="Postfach wechseln" className={`flex w-full items-center rounded-md border border-white/10 bg-white/[0.05] outline-none transition-colors hover:bg-white/[0.07] ${compact ? "h-11 justify-center p-1" : "h-[54px] gap-3 px-1.5 pr-3.5"}`} />}>
        <span className={`flex shrink-0 items-center justify-center rounded-md border border-white/10 bg-[#ff4d00] text-xs font-medium text-black ${compact ? "size-[34px]" : "size-10"}`}>{active ? initials(active.name) : <Plus className="size-4" />}</span>
        {!compact && <><span className="min-w-0 flex-1 truncate text-left text-sm text-[#a8a8a8]">{active ? active.username : "Postfach verbinden"}</span><ChevronsUpDown className="size-4 text-[#777]" /></>}
      </DropdownMenuTrigger>
      <DropdownMenuContent side={compact ? "right" : "top"} align="start" className="min-w-[250px]">
        {accounts.map((account) => <DropdownMenuItem key={account.id} onClick={() => onSelectAccount(account.id)} className="justify-between gap-2">
          <span className="flex min-w-0 items-center gap-2"><span className="flex size-7 shrink-0 items-center justify-center rounded bg-[#ff4d00] text-[10px] text-black">{initials(account.name)}</span><span className="truncate">{account.name}</span></span>
          {active && account.id === active.id && <Check className="size-4 shrink-0" />}
        </DropdownMenuItem>)}
        <DropdownMenuItem onClick={() => setAddOpen(true)}><Plus />Postfach hinzufügen</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
    <AddAccount refresh={refresh} open={addOpen} onOpenChange={setAddOpen} onCreated={onSelectAccount} hideTrigger />
  </div>
}

function WindowControls() {
  const tauri = isTauri()
  // Windows-Standard: maximiert zeigt das „Wiederherstellen“-Doppelsymbol,
  // im Fenstermodus das einfache Quadrat.
  const [maximized, setMaximized] = useState(false)
  useEffect(() => {
    if (!tauri) return
    const win = getCurrentWindow()
    let unlisten: (() => void) | undefined
    let disposed = false
    void win.isMaximized().then((value) => { if (!disposed) setMaximized(value) }).catch(() => {})
    void win.onResized(() => { void win.isMaximized().then((value) => setMaximized(value)).catch(() => {}) }).then((stop) => { if (disposed) stop(); else unlisten = stop }).catch(() => {})
    return () => { disposed = true; unlisten?.() }
  }, [tauri])
  if (!tauri) return null
  const win = getCurrentWindow()
  const base = "flex h-9 w-[46px] items-center justify-center text-[#c4c4c4] transition-colors hover:bg-white/[0.08] hover:text-white"
  return (
    <div className="absolute right-0 top-0 z-50 flex h-9 items-center">
      <button type="button" className={base} onClick={() => void win.minimize()} aria-label="Minimieren"><Minus className="size-4" /></button>
      <button type="button" className={base} onClick={() => void win.toggleMaximize()} aria-label={maximized ? "Wiederherstellen" : "Maximieren"}>{maximized ? <Copy className="size-3.5" /> : <Square className="size-[11px]" />}</button>
      <button type="button" className={`${base} hover:bg-[#e81123] hover:text-white`} onClick={() => void win.close()} aria-label="Schließen"><X className="size-4" /></button>
    </div>
  )
}

function Header({ page }: { page: Page }) {
  const titles: Record<Page, string> = { dashboard: "Übersicht", review: "Zuordnung", notifications: "Benachrichtigungen", settings: "Einstellungen" }
  return <header className="mx-auto flex h-[100px] w-full max-w-[1500px] items-start justify-between gap-6 px-14 pt-12 max-[639px]:px-7">
    <h1 className="pt-1 text-2xl font-medium tracking-tight">{titles[page]}</h1>
    {page === "settings" && <div className="relative h-[52px] w-[280px] shrink-0"><Input aria-label="Einstellungen durchsuchen" placeholder="Durchsuchen" className="h-full rounded-md border-white/10 bg-white/[0.05] px-3.5 pr-11 text-sm placeholder:text-[#888]" /><Search className="pointer-events-none absolute right-3.5 top-1/2 size-4 -translate-y-1/2 text-[#888]" /></div>}
  </header>
}

function Dashboard({ summary, onReview, scrollRef, agentOnline, dailyStats, notifications: recentNotifications, onNotifications }: { summary: Summary; onReview: () => void; scrollRef: React.RefObject<HTMLElement | null>; agentOnline: boolean; dailyStats: DailyStat[] | null; notifications?: NotificationItem[]; onNotifications?: () => void }) {
  const [period, setPeriod] = useState(() => readStoredValue("mailmune.dashboardPeriod", "spamalytic.dashboardPeriod", "Gesamt"))
  useEffect(() => { localStorage.setItem("mailmune.dashboardPeriod", period) }, [period])
  const [showInbox, setShowInbox] = useState(true)
  const [showFalsePositives, setShowFalsePositives] = useState(true)
  // „Nicht erkannt“: Spam, den Mensch/Fremdfilter einsortiert haben. Serie ist
  // per Legende abschaltbar wie Eingang und Fehlalarme.
  const [showMissed, setShowMissed] = useState(true)
  // Echte Agent-Daten in der Desktop-App; Demo-Daten nur in der Browser-Vorschau.
  const activeChartData = dailyStats ? buildRealChartData(dailyStats, period) : chartDataByPeriod[period]
  const totals = activeChartData.reduce((sum, item) => ({ spam: sum.spam + item.spam, inbox: sum.inbox + item.inbox, falsePositive: sum.falsePositive + item.falsePositive, missed: sum.missed + item.missed }), { spam: 0, inbox: 0, falsePositive: 0, missed: 0 })
  const spamShare = totals.inbox > 0 ? Math.round((totals.spam / totals.inbox) * 100) : 0
  return <div className="dashboard-cards space-y-6">
    {isTauri() && !agentOnline && <p role="status" className="rounded-lg border border-white/[0.08] bg-white/[0.03] px-4 py-3 text-xs text-[#999]">Der lokale Agent ist noch nicht erreichbar. Sobald er läuft, erscheinen hier echte Daten.</p>}
    {isTauri() && agentOnline && summary.accounts === 0 && <p role="status" className="rounded-lg border border-white/[0.08] bg-white/[0.03] px-4 py-3 text-xs text-[#999]">Noch kein Postfach verbunden. Füge in den Einstellungen ein Postfach hinzu, um den lesenden Trockenlauf zu starten.</p>}
    <div data-section-id="dashboard-summary" className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <Metric label="Spam zugeordnet" value={summary.moved + summary.confirmed} note="Diese Woche" />
      <Metric label="Noch zu prüfen" value={summary.pending} note="Menschliche Entscheidung" onClick={onReview} />
      <Metric label="Verarbeitet" value={summary.scanned} note="Gesamt, einmalig" />
      <Metric label="Fehlalarmrate" value={`${(summary.falsePositiveRate * 100).toFixed(1)} %`} note="Bestätigte Prüfungen" />
    </div>
    <div data-section-id="dashboard-analysis" className="grid gap-6 xl:grid-cols-[1.45fr_1fr]">
      <Card className="relative min-w-0 border-white/[0.07] bg-[#1b1b1b] shadow-none"><CardHeader className="pr-[340px]"><div><CardTitle>Spam</CardTitle><CardDescription>Spam im Verhältnis zum normalen Eingang</CardDescription></div><div className="absolute right-6 top-6"><Segmented options={["Tag", "Woche", "Monat", "Jahr", "Gesamt"]} value={period} onChange={setPeriod} /></div></CardHeader><CardContent className="min-w-0 pt-2"><div className="mb-4 flex flex-wrap items-end justify-between gap-4"><div className="flex items-baseline gap-3"><span className="text-3xl font-medium text-[#ff6b2c]">{spamShare} %</span><span className="text-xs text-[#777]">Spam · {totals.spam.toLocaleString("de-DE")} Nachrichten</span></div><div className="flex flex-wrap gap-2 text-xs"><span className="flex items-center gap-2 rounded-md px-2.5 py-1.5 text-[#ff6b2c]"><span className="size-2 rounded-full bg-[#ff6b2c]" />Spam</span><button onClick={() => setShowInbox((value) => !value)} className={`flex items-center gap-2 rounded-md px-2.5 py-1.5 transition-colors ${showInbox ? "bg-white/[0.05] text-[#d6d6d6]" : "text-[#555]"}`}><span className={`size-2 rounded-full ${showInbox ? "bg-[#d6d6d6]" : "bg-[#555]"}`} />Eingang · {totals.inbox.toLocaleString("de-DE")}</button><button onClick={() => setShowFalsePositives((value) => !value)} className={`flex items-center gap-2 rounded-md px-2.5 py-1.5 transition-colors ${showFalsePositives ? "bg-white/[0.05] text-[#858585]" : "text-[#4d4d4d]"}`}><span className={`size-2 rounded-full ${showFalsePositives ? "bg-[#858585]" : "bg-[#4d4d4d]"}`} />Fehlalarme · {totals.falsePositive.toLocaleString("de-DE")}</button><button onClick={() => setShowMissed((value) => !value)} aria-pressed={showMissed} className={`flex items-center gap-2 rounded-md px-2.5 py-1.5 transition-colors ${showMissed ? "bg-white/[0.05] text-[#e0a86c]" : "text-[#4d4d4d]"}`}><span className={`size-2 rounded-full ${showMissed ? "bg-[#e0a86c]" : "bg-[#4d4d4d]"}`} />Nicht erkannt · {totals.missed.toLocaleString("de-DE")}</button></div></div><div className="h-[240px]"><ResponsiveContainer width="100%" height="100%" minWidth={0} minHeight={0} initialDimension={{ width: 640, height: 240 }}><AreaChart data={activeChartData}><defs><linearGradient id="spamArea" x1="0" y1="0" x2="0" y2="1"><stop offset="5%" stopColor="#ff6b2c" stopOpacity={0.42} /><stop offset="95%" stopColor="#ff6b2c" stopOpacity={0.02} /></linearGradient><linearGradient id="inboxArea" x1="0" y1="0" x2="0" y2="1"><stop offset="5%" stopColor="#d6d6d6" stopOpacity={0.16} /><stop offset="95%" stopColor="#d6d6d6" stopOpacity={0.01} /></linearGradient></defs><CartesianGrid vertical={false} stroke="rgba(255,255,255,.055)" /><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ fill: "#777", fontSize: 12 }} /><YAxis axisLine={false} tickLine={false} tick={{ fill: "#666", fontSize: 11 }} width={34} domain={[0, "auto"]} /><ChartTooltip cursor={false} contentStyle={{ background: "#242424", border: "1px solid rgba(255,255,255,.1)", borderRadius: 8, fontSize: 12 }} />{showInbox && <Area type="monotone" dataKey="inbox" name="Eingang" fill="url(#inboxArea)" stroke="#d6d6d6" strokeWidth={1.5} dot={false} />}{showFalsePositives && <Area type="monotone" dataKey="falsePositive" name="Fehlalarme" fill="transparent" stroke="#858585" strokeWidth={1.5} strokeDasharray="4 4" dot={false} />}{showMissed && <Area type="monotone" dataKey="missed" name="Nicht erkannt" fill="transparent" stroke="#e0a86c" strokeWidth={1.5} strokeDasharray="2 3" dot={false} />}<Area type="monotone" dataKey="spam" name="Spam" fill="url(#spamArea)" stroke="#ff6b2c" strokeWidth={2} dot={false} activeDot={{ r: 4, fill: "#ff6b2c" }} /></AreaChart></ResponsiveContainer></div></CardContent></Card>
      <Card className="border-white/[0.07] bg-[#1b1b1b] shadow-none"><CardHeader><CardTitle>Letzte Benachrichtigungen</CardTitle><CardDescription>Lokale Ereignisse und offene Aufgaben</CardDescription></CardHeader><CardContent className="divide-y divide-white/[0.06]">{(recentNotifications ?? notifications).map((item) => <button key={item.id} type="button" onClick={onNotifications} className="flex w-full gap-3 py-4 text-left first:pt-0 transition-opacity hover:opacity-80"><div className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg bg-white/[0.05]">{item.action ? <BellDot className="size-4" /> : <Check className="size-4 text-[#999]" />}</div><div className="min-w-0 flex-1"><p className="truncate text-sm font-medium">{item.title}</p><p className="mt-1 truncate text-xs leading-5 text-[#888]">{item.detail}</p><p className="mt-2 text-[11px] text-[#555]">{item.time}</p></div></button>)}</CardContent></Card>
    </div>
    <SectionIndicator items={[{ id: "dashboard-summary", label: "Kennzahlen" }, { id: "dashboard-analysis", label: "Analyse" }]} scrollRef={scrollRef} />
  </div>
}

function Metric({ label, value, note, onClick }: { label: string; value: string | number; note: string; onClick?: () => void }) {
  return <button disabled={!onClick} onClick={onClick} className="rounded-xl border border-white/[0.07] bg-[#1b1b1b] p-5 text-left disabled:cursor-default"><p className="text-xs text-[#777]">{label}</p><p className="mt-3 text-3xl font-medium tracking-tight">{value}</p><p className="mt-2 text-xs text-[#5f5f5f]">{note}</p></button>
}

function Segmented({ options, value, onChange }: { options: string[]; value: string; onChange: (value: string) => void }) {
  return <div className="flex rounded-lg border border-white/[0.07] bg-[#242424] p-1">{options.map((option) => <button key={option} onClick={() => onChange(option)} className={`rounded-md px-2.5 py-1.5 text-xs ${value === option ? "bg-[#171717] text-white" : "text-[#777] hover:text-white"}`}>{option}</button>)}</div>
}

// Date helpers for the custom range picker (plain JS, no extra dependency).
// ISO strings (YYYY-MM-DD) compare lexicographically in chronological order.
type MonthView = { year: number; month: number }
function monthOf(iso?: string | null): MonthView {
  const parsed = iso ? new Date(iso) : new Date()
  const base = Number.isNaN(parsed.getTime()) ? new Date() : parsed
  return { year: base.getFullYear(), month: base.getMonth() }
}
function shiftMonth(view: MonthView, delta: number): MonthView {
  const total = view.year * 12 + view.month + delta
  return { year: Math.floor(total / 12), month: ((total % 12) + 12) % 12 }
}
function toISO(year: number, month: number, day: number): string {
  return `${year}-${String(month + 1).padStart(2, "0")}-${String(day).padStart(2, "0")}`
}
// Returns 6 weeks of cells (leading/trailing blanks are null), Monday-first.
function buildMonthCells(year: number, month: number): (number | null)[] {
  const firstWeekday = (new Date(year, month, 1).getDay() + 6) % 7
  const daysInMonth = new Date(year, month + 1, 0).getDate()
  const cells: (number | null)[] = []
  for (let index = 0; index < firstWeekday; index++) cells.push(null)
  for (let day = 1; day <= daysInMonth; day++) cells.push(day)
  while (cells.length % 7 !== 0) cells.push(null)
  return cells
}
function formatShortDate(iso: string): string {
  const [year, month, day] = iso.split("-")
  return `${day}.${month}.${year}`
}

function DateRangeDialog({ open, onOpenChange, initial, onApply }: { open: boolean; onOpenChange: (open: boolean) => void; initial: { from: string; to: string } | null; onApply: (range: { from: string; to: string }) => void }) {
  const [viewMonth, setViewMonth] = useState<MonthView>(() => monthOf(initial?.from))
  const [from, setFrom] = useState(initial?.from ?? "")
  const [to, setTo] = useState(initial?.to ?? "")
  useEffect(() => {
    if (!open) return
    setFrom(initial?.from ?? "")
    setTo(initial?.to ?? "")
    setViewMonth(monthOf(initial?.from))
  }, [open, initial])
  const pick = (day: string) => {
    if (!from || (from && to)) { setFrom(day); setTo("") }
    else if (day < from) { setTo(from); setFrom(day) }
    else { setTo(day) }
  }
  const cells = buildMonthCells(viewMonth.year, viewMonth.month)
  const label = new Intl.DateTimeFormat("de-DE", { month: "long", year: "numeric" }).format(new Date(viewMonth.year, viewMonth.month, 1))
  const apply = () => { if (!from) return; onApply({ from, to: to || from }); onOpenChange(false) }
  const navButton = "flex size-8 items-center justify-center rounded-md text-[#aaa] transition-colors hover:bg-white/[0.06] hover:text-white"
  const inputClass = "mt-1 h-10 w-full rounded-md border border-white/10 bg-[#242424] px-3 text-sm text-white outline-none [color-scheme:dark] focus:border-white/25"
  return <Dialog open={open} onOpenChange={onOpenChange}>
    <DialogContent className="border-white/[0.08] bg-[#1d1d1d] sm:max-w-[360px]">
      <DialogHeader><DialogTitle>Benutzerdefinierter Zeitraum</DialogTitle><DialogDescription>Von und Bis per Klick im Kalender wählen oder unten direkt eingeben.</DialogDescription></DialogHeader>
      <div className="py-1">
        <div className="mb-2 flex items-center justify-between">
          <button type="button" onClick={() => setViewMonth(shiftMonth(viewMonth, -1))} aria-label="Vorheriger Monat" className={navButton}><ChevronLeft className="size-4" /></button>
          <span className="text-sm font-medium">{label}</span>
          <button type="button" onClick={() => setViewMonth(shiftMonth(viewMonth, 1))} aria-label="Nächster Monat" className={navButton}><ChevronRight className="size-4" /></button>
        </div>
        <div className="grid grid-cols-7 gap-1 text-center text-[11px] text-[#666]">{["Mo", "Di", "Mi", "Do", "Fr", "Sa", "So"].map((weekday) => <div key={weekday} className="py-1">{weekday}</div>)}</div>
        <div className="mt-1 grid grid-cols-7 gap-1">
          {cells.map((cell, index) => {
            if (cell === null) return <div key={index} />
            const iso = toISO(viewMonth.year, viewMonth.month, cell)
            const endpoint = iso === from || iso === to
            const between = !!from && !!to && iso > from && iso < to
            return <button key={index} type="button" onClick={() => pick(iso)} className={`flex size-9 items-center justify-center rounded-md text-xs transition-colors ${endpoint ? "bg-[#ff6b2c] text-white" : between ? "bg-white/[0.08] text-white" : "text-[#a8a8a8] hover:bg-white/[0.06]"}`}>{cell}</button>
          })}
        </div>
        <div className="mt-4 flex items-end gap-3">
          <label className="flex-1 text-xs text-[#888]">Von<input type="date" value={from} max={to || undefined} onChange={(event) => setFrom(event.target.value)} className={inputClass} /></label>
          <label className="flex-1 text-xs text-[#888]">Bis<input type="date" value={to} min={from || undefined} onChange={(event) => setTo(event.target.value)} className={inputClass} /></label>
        </div>
      </div>
      <DialogFooter><Button variant="ghost" onClick={() => onOpenChange(false)}>Abbrechen</Button><Button disabled={!from} onClick={apply}>Übernehmen</Button></DialogFooter>
    </DialogContent>
  </Dialog>
}

// ScoreRangeFilter is a compact Von/Bis slider pair (0-100 %) that narrows the
// review table by decision score. Two native range inputs keep it dependency-
// free; min never exceeds max and vice versa.
// DualRangeSlider is a two-thumb 0-100% range slider styled like the settings
// sliders (dark track, striped background, a filled selected range and a thumb
// on each side). Custom pointer handling keeps it dependency-free and avoids
// the native orange range input.
function DualRangeSlider({ min, max, onChange }: { min: number; max: number; onChange: (min: number, max: number) => void }) {
  const trackRef = useRef<HTMLDivElement>(null)
  const [dragging, setDragging] = useState<"min" | "max" | null>(null)
  useEffect(() => {
    if (!dragging) return
    const move = (event: PointerEvent) => {
      const rect = trackRef.current?.getBoundingClientRect()
      if (!rect || rect.width === 0) return
      const value = Math.round(Math.max(0, Math.min(100, ((event.clientX - rect.left) / rect.width) * 100)))
      if (dragging === "min") onChange(Math.min(value, max), max)
      else onChange(min, Math.max(value, min))
    }
    const stop = () => setDragging(null)
    window.addEventListener("pointermove", move)
    window.addEventListener("pointerup", stop)
    return () => { window.removeEventListener("pointermove", move); window.removeEventListener("pointerup", stop) }
  }, [dragging, min, max, onChange])
  // Exakt dieselbe Geometrie wie der Einstellungs-Slider: Track mit p-1,
  // Ticks auf dem Track, Füll-„Schuh“ UND Griffe in der inneren Content-Box
  // (damit Prozentwerte identisch auflösen), Griff-Balken mit 12 px Abstand
  // innerhalb der Füllkanten.
  const thumb = "absolute top-1/2 z-10 h-5 w-1 -translate-y-1/2 cursor-ew-resize rounded-full bg-[#242424] before:absolute before:-inset-x-2.5 before:-inset-y-2"
  return (
    <div className="relative h-12 overflow-hidden rounded-[10px] border border-white/10 bg-[#242424] p-1">
      <div className="pointer-events-none absolute inset-y-3 left-4 right-4 opacity-70" style={{ backgroundImage: "repeating-linear-gradient(90deg, rgba(255,255,255,.055) 0 4px, transparent 4px 14px)" }} />
      <div ref={trackRef} className="relative h-full">
        <div className="pointer-events-none absolute inset-y-0 rounded-lg bg-[#171717]" style={{ left: `${min}%`, width: `${Math.max(max - min, 0)}%` }} />
        <span role="slider" aria-label="Score von" aria-valuenow={min} aria-valuemin={0} aria-valuemax={100} tabIndex={0} onPointerDown={() => setDragging("min")} onKeyDown={(event) => { if (event.key === "ArrowLeft") onChange(Math.max(0, min - 1), max); if (event.key === "ArrowRight") onChange(Math.min(max, min + 1), max) }} className={thumb} style={{ left: `calc(${min}% + 12px)` }} />
        <span role="slider" aria-label="Score bis" aria-valuenow={max} aria-valuemin={0} aria-valuemax={100} tabIndex={0} onPointerDown={() => setDragging("max")} onKeyDown={(event) => { if (event.key === "ArrowLeft") onChange(min, Math.max(min, max - 1)); if (event.key === "ArrowRight") onChange(min, Math.min(100, max + 1)) }} className={thumb} style={{ left: `calc(${max}% - 16px)` }} />
      </div>
    </div>
  )
}

// ScoreRangeDialog ist das Score-Pendant zum Datumsfilter: gleicher Dialog-
// Aufbau (Beschriftung oben links, Wertanzeige oben rechts), Von/Bis-Zahlen
// unten außen. Übernahme erst per „Anwenden“.
function ScoreRangeDialog({ open, onOpenChange, initial, onApply }: { open: boolean; onOpenChange: (open: boolean) => void; initial: { min: number; max: number }; onApply: (range: { min: number; max: number }) => void }) {
  const [draft, setDraft] = useState(initial)
  useEffect(() => { if (open) setDraft(initial) }, [open, initial])
  return <Dialog open={open} onOpenChange={onOpenChange}>
    <DialogContent className="border-white/[0.08] bg-[#1d1d1d] sm:max-w-[440px]">
      <DialogHeader><DialogTitle>Score-Bereich</DialogTitle><DialogDescription>Nur Nachrichten innerhalb dieses Score-Bereichs in der Tabelle anzeigen.</DialogDescription></DialogHeader>
      <div className="py-2">
        <div className="mb-3 flex items-baseline justify-between"><span className="text-sm text-[#ccc]">Score von/bis</span><span className="font-mono text-sm text-white tabular-nums">{draft.min}–{draft.max} %</span></div>
        <DualRangeSlider min={draft.min} max={draft.max} onChange={(min, max) => setDraft({ min, max })} />
        <div className="mt-2 flex justify-between font-mono text-xs text-[#888] tabular-nums"><span>0 %</span><span>100 %</span></div>
      </div>
      <DialogFooter className="border-t border-white/[0.09] pt-4"><Button variant="ghost" className="mr-auto" onClick={() => setDraft({ min: 0, max: 100 })}>Zurücksetzen</Button><Button variant="ghost" onClick={() => onOpenChange(false)}>Abbrechen</Button><Button onClick={() => onApply(draft)}>Anwenden</Button></DialogFooter>
    </DialogContent>
  </Dialog>
}

function ReviewPage({ decisions, refresh, agentOnline, onCheckMail }: { decisions: Decision[]; refresh: () => void; agentOnline: boolean; onCheckMail?: () => void }) {
  const tableScrollRef = useRef<HTMLDivElement>(null)
  const tableFade = useScrollFade(tableScrollRef)
  const tableHorizontalFade = useScrollFade(tableScrollRef, "horizontal")
  const toolbarScrollRef = useRef<HTMLDivElement>(null)
  const toolbarFade = useScrollFade(toolbarScrollRef, "horizontal")
  const [view, setView] = useState<"review" | "spam">(() => (readStoredValue("mailmune.reviewView", "spamalytic.reviewView", "review") as "review" | "spam"))
  const [reviewFilter, setReviewFilter] = useState<"review" | "rejected">(() => (readStoredValue("mailmune.reviewFilter", "spamalytic.reviewFilter", "review") as "review" | "rejected"))
  // Default to "all": a first scan of an older mailbox surfaces many pending
  // decisions whose receivedAt is far in the past. A narrow default (e.g.
  // "week") would hide them and look like data loss, so the review backlog is
  // shown in full unless the user explicitly narrows it. The v2 key resets the
  // previous "week" default for existing installs.
  const [range, setRange] = useState<Range>(() => (readStoredValue("mailmune.reviewRange.v2", "spamalytic.reviewRange", "all") as Range))
  const [customRange, setCustomRange] = useState<{ from: string; to: string } | null>(() => {
    try { const raw = localStorage.getItem("mailmune.reviewCustomRange"); return raw ? (JSON.parse(raw) as { from: string; to: string }) : null } catch { return null }
  })
  const [customOpen, setCustomOpen] = useState(false)
  // Merker: „Anwenden“ im Kalender schließt den Dialog selbst – der
  // Close-Handler darf den frischen Zeitraum nicht sofort zurücksetzen
  // (Bug: Filter griff erst nach zweimaligem Anwenden).
  const customApplied = useRef(false)
  const [scoreRange, setScoreRange] = useState<{ min: number; max: number }>(() => {
    try { const raw = localStorage.getItem("mailmune.reviewScoreRange"); return raw ? (JSON.parse(raw) as { min: number; max: number }) : { min: 0, max: 100 } } catch { return { min: 0, max: 100 } }
  })
  const [scoreOpen, setScoreOpen] = useState(false)
  const [filterOpen, setFilterOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [selected, setSelected] = useState<string[]>([])
  const [referenceTime] = useState(() => Date.now())
  // Sortierung je Ansicht persistent (übersteht Seitenwechsel).
  const [sort, setSort] = useState<{ key: SortKey; direction: SortDirection }>(() => readStoredSort(view))
  const scoreFilterActive = scoreRange.min > 0 || scoreRange.max < 100
  const filtersActive = range !== "all" || scoreFilterActive || (view === "review" && reviewFilter !== "review")
  useEffect(() => { localStorage.setItem("mailmune.reviewView", view) }, [view])
  useEffect(() => { localStorage.setItem("mailmune.reviewFilter", reviewFilter) }, [reviewFilter])
  useEffect(() => { localStorage.setItem("mailmune.reviewRange.v2", range) }, [range])
  useEffect(() => { localStorage.setItem("mailmune.reviewCustomRange", JSON.stringify(customRange)) }, [customRange])
  useEffect(() => { localStorage.setItem("mailmune.reviewScoreRange", JSON.stringify(scoreRange)) }, [scoreRange])
  useEffect(() => { setSort(readStoredSort(view)) }, [view])
  useEffect(() => { localStorage.setItem(`mailmune.sort.${view}`, JSON.stringify(sort)) }, [sort, view])
  const resetFilters = () => { setRange("all"); setReviewFilter("review"); setScoreRange({ min: 0, max: 100 }); setCustomRange(null); setSelected([]) }
  const filtered = useMemo(() => {
    const days = range === "week" ? 7 : range === "month" ? 31 : range === "year" ? 366 : Infinity
    const customFrom = range === "custom" && customRange ? new Date(customRange.from + "T00:00:00").getTime() : null
    const customTo = range === "custom" && customRange ? new Date(customRange.to + "T23:59:59.999").getTime() : null
    const inRange = (receivedAt: string) => {
      const time = new Date(receivedAt).getTime()
      if (customFrom !== null && customTo !== null) return time >= customFrom && time <= customTo
      return referenceTime - time <= days * 86_400_000
    }
    // TESTMODUS: Der Kandidaten-Filter (score >= 0.6) ist bewusst entfernt,
    // damit jede gespeicherte Nachricht – auch nicht markierte – sichtbar ist
    // und inspectiert werden kann. TODO(revert): `item.score >= 0.6 &&` vor dem
    // Status-Filter wieder einfuegen vor dem Release (siehe TODO.md „Testmodus").
    return decisions.filter((item) => (view === "review" ? (reviewFilter === "review" ? ["pending", "moved"].includes(item.status) : item.status === "rejected") : item.status === "confirmed") && inRange(item.receivedAt) && item.score * 100 >= scoreRange.min && item.score * 100 <= scoreRange.max && `${item.from} ${item.subject}`.toLowerCase().includes(query.toLowerCase())).sort((a, b) => {
      if (!sort.direction) return 0; const left = sort.key === "category" ? spamCategory(a) : a[sort.key]; const right = sort.key === "category" ? spamCategory(b) : b[sort.key]; const result = typeof left === "number" ? left - Number(right) : String(left).localeCompare(String(right), "de"); return sort.direction === "asc" ? result : -result
    })
  }, [decisions, view, reviewFilter, range, customRange, scoreRange, query, sort, referenceTime])
  // Auswahl als Set: O(1) statt O(n) pro Zeile, damit Auswahlwechsel die
  // Sammelaktions-Leiste ohne spürbare Verzögerung erscheinen lassen.
  const selectedSet = useMemo(() => new Set(selected), [selected])
  const [details, setDetails] = useState<Decision | null>(null)
  const toggleAll = () => setSelected(selected.length === filtered.length ? [] : filtered.map((item) => item.id))
  const changeView = (nextView: "review" | "spam") => { setSelected([]); setView(nextView) }
  const review = async (action: "confirm" | "reject") => {
    if (!selected.length) return
    try { await agentRequest("POST", "/v1/reviews", { decisionIds: selected, action, idempotencyKey: crypto.randomUUID() }); setSelected([]); await refresh() } catch { setSelected([]) }
  }
  return <div className="relative flex h-[calc(100vh-3rem)] min-h-0 flex-col overflow-hidden pb-4">
    <div className="flex shrink-0 items-start justify-between gap-6">
      <h1 className="pt-1 text-2xl font-medium tracking-tight">Zuordnung</h1>
      <ViewSwitch value={view} onChange={changeView} />
    </div>
    <div className="mt-12 flex min-h-0 flex-1 flex-col">
      <div className="relative shrink-0"><div ref={toolbarScrollRef} className="flex items-center gap-2 overflow-x-auto overflow-y-hidden pb-1 [scrollbar-gutter:stable]">
        <ToolbarButton iconOnly onClick={() => { void refresh(); onCheckMail?.() }} aria-label="Daten aktualisieren und neue Mails prüfen"><RefreshCw className="size-4" /></ToolbarButton>
        <div className="relative h-[52px] w-[300px] min-w-[220px] shrink-0">
          <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Durchsuchen" className="h-full rounded-md border-white/10 bg-white/[0.05] px-3.5 pr-11 text-sm placeholder:text-[#888]" />
          <Search className="pointer-events-none absolute right-3.5 top-1/2 size-4 -translate-y-1/2 text-[#888]" />
        </div>
        <div className="flex shrink-0 items-center">
          <FilterToolbarButton active={filtersActive} open={filterOpen} count={filtered.length} onToggle={() => setFilterOpen((current) => !current)} onReset={resetFilters} />
          {filterOpen && view === "review" && <><ToolbarConnector /><Select value={reviewFilter} onValueChange={(value) => { setSelected([]); setReviewFilter(value as "review" | "rejected") }}><SelectTrigger className={`h-[52px]! min-w-[148px] shrink-0 rounded-md px-3.5 text-sm! ${reviewFilter !== "review" ? "border-white! bg-white! text-[#171717]! hover:bg-white/90! [&_svg]:text-[#171717]!" : "border-white/10 bg-white/[0.05] text-[#aaa]"}`}><SelectValue>{reviewFilter === "review" ? "Review" : "Kein Spam"}</SelectValue></SelectTrigger><SelectContent><SelectItem value="review">Review</SelectItem><SelectItem value="rejected">Kein Spam</SelectItem></SelectContent></Select></>}
          {filterOpen && <><ToolbarConnector /><Select value={range} onValueChange={(value) => { if (value === "custom") { setRange("custom"); setCustomOpen(true) } else { setSelected([]); setRange(value as Range) } }}><SelectTrigger className={`h-[52px]! min-w-[148px] shrink-0 rounded-md px-3.5 text-sm! ${range !== "all" ? "border-white! bg-white! text-[#171717]! hover:bg-white/90! [&_svg]:text-[#171717]!" : "border-white/10 bg-white/[0.05] text-[#aaa]"}`}><SelectValue>{({ week: "Woche", month: "Monat", year: "Jahr", all: "Alles", custom: "Benutzerdefiniert" } as const)[range]}</SelectValue></SelectTrigger><SelectContent><SelectItem value="week">Woche</SelectItem><SelectItem value="month">Monat</SelectItem><SelectItem value="year">Jahr</SelectItem><SelectItem value="all">Alles</SelectItem><SelectItem value="custom">Benutzerdefiniert</SelectItem></SelectContent></Select></>}
          {filterOpen && <><ToolbarConnector /><button onClick={() => setScoreOpen(true)} className={`flex h-[52px] shrink-0 items-center gap-2 rounded-md border px-3.5 text-sm transition-colors ${scoreFilterActive || scoreOpen ? "border-white! bg-white! text-[#171717]! hover:bg-white/90" : "border-white/10 bg-white/[0.05] text-[#aaa] hover:bg-white/[0.08] hover:text-white"}`}><Gauge className="size-4" />Score<span className="inline-block min-w-[64px] font-mono text-xs tabular-nums opacity-70">{scoreRange.min}–{scoreRange.max}%</span></button></>}
          <ScoreRangeDialog open={scoreOpen} onOpenChange={setScoreOpen} initial={scoreRange} onApply={(picked) => { setSelected([]); setScoreRange(picked); setScoreOpen(false) }} />
          {range === "custom" && <button onClick={() => setCustomOpen(true)} className="ml-2 flex h-[52px] shrink-0 items-center gap-2 rounded-md border border-white! bg-white! px-3.5 text-sm text-[#171717]! transition-colors hover:bg-white/90"><CalendarDays className="size-4" />{customRange ? `${formatShortDate(customRange.from)} – ${formatShortDate(customRange.to)}` : "Zeitraum wählen"}</button>}
          <DateRangeDialog open={customOpen} onOpenChange={(next) => { setCustomOpen(next); if (!next) { if (customApplied.current) { customApplied.current = false; return } if (!customRange && range === "custom") setRange("all") } }} initial={customRange} onApply={(picked) => { customApplied.current = true; setCustomRange(picked); setRange("custom"); setSelected([]) }} />
          {/* Nachrichtendetails: vollständige Reason-Codes der Entscheidung.
              Rohe Mailtexte werden aus Datenschutzgründen nicht gespeichert. */}
          <Dialog open={Boolean(details)} onOpenChange={(open) => { if (!open) setDetails(null) }}>
            <DialogContent className="border-white/[0.08] bg-[#1d1d1d] sm:max-w-[520px]">
              <DialogHeader><DialogTitle className="break-words pr-8">{details?.subject || "(ohne Betreff)"}</DialogTitle><DialogDescription>{details?.from}{details ? ` · ${new Intl.DateTimeFormat("de-DE", { day: "2-digit", month: "2-digit", year: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(details.receivedAt))} · ${details.currentFolder}` : ""}</DialogDescription></DialogHeader>
              {details && <div className="space-y-3 py-1">
                <div className="flex items-center justify-between rounded-[10px] border border-white/[0.08] bg-white/[0.03] px-3.5 py-2.5 text-sm"><span className="text-[#888]">Score</span><span className="font-mono" style={{ color: scoreColor(details.score) }}>{Math.round(details.score * 100)} %</span></div>
                <div className="space-y-1.5">
                  {details.evidence.length === 0 && <p className="rounded-[10px] border border-white/[0.08] px-3 py-2 text-xs text-[#666]">Keine Auffälligkeiten – unter allen Schwellen.</p>}
                  {details.evidence.map((entry, index) => <div key={index} className="flex items-start gap-2 rounded-[10px] border border-white/[0.08] bg-white/[0.02] px-3 py-2 text-xs">
                    <span className="mt-px shrink-0 rounded-[4px] border border-white/10 px-1.5 py-0.5 font-mono text-[10px] text-[#888]">{entry.code}</span>
                    <span className="min-w-0 flex-1 leading-5 text-[#bbb]">{entry.summary}</span>
                    <span className="shrink-0 font-mono text-[#888]">{entry.weight > 0 ? "+" : ""}{entry.weight.toFixed(2)}</span>
                  </div>)}
                </div>
                <p className="text-[11px] leading-4 text-[#666]">Aus Datenschutzgründen speichert Mailmune keine Nachrichtentexte. Die Einordnung basiert auf Metadaten und begrenzten Textmerkmalen zum Scan-Zeitpunkt; die E-Mail selbst bleibt unverändert im Postfach.</p>
              </div>}
              <DialogFooter><Button variant="outline" onClick={() => setDetails(null)}>Schließen</Button></DialogFooter>
            </DialogContent>
          </Dialog>
        </div>
      </div><ScrollFade strength={toolbarFade} direction="horizontal" compact targetRef={toolbarScrollRef} /></div>
      <div className="relative mt-6 min-h-0 flex-1">
        <div ref={tableScrollRef} className="h-full overflow-auto overscroll-contain [scrollbar-gutter:stable]">
          <div className="min-w-[1224px]">
            <div role="row" className="sticky top-0 z-20 grid h-[52px] grid-cols-[56px_264px_130px_170px_274px_170px_160px] items-center rounded-md border border-white/10 bg-[#232323]">
              <div role="columnheader" className={`flex h-full items-center px-4 ${shortDivider}`}><Checkbox checked={filtered.length > 0 && selected.length === filtered.length} onCheckedChange={toggleAll} aria-label="Alle auswählen" /></div><SortableHead icon={Mail} label="Absender" name="from" sort={sort} setSort={setSort} /><SortableHead icon={Gauge} label="Score" name="score" sort={sort} setSort={setSort} /><SortableHead icon={Tag} label="Kategorie" name="category" sort={sort} setSort={setSort} /><SortableHead icon={Text} label="Betreff" name="subject" sort={sort} setSort={setSort} /><SortableHead icon={CalendarDays} label="Datum" name="receivedAt" sort={sort} setSort={setSort} /><SortableHead icon={CircleDot} label="Status" name="status" sort={sort} setSort={setSort} last />
            </div>
            <Table containerClassName="overflow-visible" className="w-[1224px] table-fixed">
              <colgroup><col className="w-[56px]" /><col className="w-[264px]" /><col className="w-[130px]" /><col className="w-[170px]" /><col className="w-[274px]" /><col className="w-[170px]" /><col className="w-[160px]" /></colgroup>
          <TableBody>{filtered.map((item) => {
            const active = selectedSet.has(item.id)
            const reason = item.evidence.map((entry) => entry.summary).join(" · ")
            return <TableRow key={item.id} data-state={active ? "selected" : undefined} className="h-16 border-white/[0.09] bg-transparent text-[#a8a8a8] hover:bg-white/[0.025] data-[state=selected]:bg-[#101010] data-[state=selected]:text-white">
              <TableCell className={`px-4 ${shortDivider}`}><Checkbox checked={active} onCheckedChange={() => setSelected((current) => current.includes(item.id) ? current.filter((id) => id !== item.id) : [...current, item.id])} /></TableCell>
              <TableCell className={`px-3 text-sm ${shortDivider}`}><div className="flex min-w-0 items-center gap-2"><Tooltip><TooltipTrigger render={<button className="flex size-7 shrink-0 items-center justify-center rounded-md text-[#666] transition-colors hover:bg-white/[0.05] hover:text-white" aria-label="Details anzeigen" onClick={() => setDetails(item)}><MailOpen className="size-3.5" /></button>} /><TooltipContent side="top">Details anzeigen</TooltipContent></Tooltip><Tooltip><TooltipTrigger render={<span className="min-w-0 truncate" tabIndex={0}>{item.from}</span>} /><TooltipContent side="top">{item.from}</TooltipContent></Tooltip></div></TableCell>
              <TableCell className={`px-4 font-mono text-xs transition-colors ${shortDivider}`} style={{ color: scoreColor(item.score) }}>{Math.round(item.score * 100)} %</TableCell>
              <TableCell className={`px-4 text-xs text-[#888] ${shortDivider}`}><span className="flex items-center gap-1.5">{item.evidence.some((entry) => entry.group === "model") && <Tooltip><TooltipTrigger render={<Bot className="size-3.5 shrink-0 text-[#888]" aria-label="von KI-geprüft" />} /><TooltipContent side="top">von KI-geprüft</TooltipContent></Tooltip>}<span className="truncate">{spamCategory(item)}</span></span></TableCell>
              <TableCell className={`px-4 ${shortDivider}`}><Tooltip><TooltipTrigger render={<p className="truncate text-sm" tabIndex={0}>{item.subject}</p>} /><TooltipContent side="top">{item.subject}</TooltipContent></Tooltip><Tooltip><TooltipTrigger render={<p className="mt-1 truncate text-xs text-[#666]" tabIndex={0}>{reason}</p>} /><TooltipContent side="top">{reason}</TooltipContent></Tooltip></TableCell>
              <TableCell className={`whitespace-nowrap px-4 text-xs text-[#888] ${shortDivider}`}>{new Intl.DateTimeFormat("de-DE", { day: "2-digit", month: "2-digit", year: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(item.receivedAt))}</TableCell>
              <TableCell className="px-4"><StatusBadge status={item.status} /></TableCell>
            </TableRow>
          })}{filtered.length === 0 && <TableRow><TableCell colSpan={7} className="h-40 text-center text-sm text-[#666]">{isTauri() && !agentOnline ? "Der lokale Agent ist noch nicht erreichbar." : "Keine Nachrichten für diese Ansicht."}</TableCell></TableRow>}</TableBody>
            </Table>
          </div>
        </div>
        <ScrollFade strength={tableFade} targetRef={tableScrollRef} />
        <ScrollFade strength={tableHorizontalFade} direction="horizontal" targetRef={tableScrollRef} />
      </div>
    </div>
    <FloatingActions visible={selected.length > 0} primary={`${view === "review" ? "Spam markieren" : "Kein Spam"} (${selected.length})`} onPrimary={() => void review(view === "review" ? "confirm" : "reject")} middle={view === "review" ? { label: `Kein Spam (${selected.length})`, onClick: () => void review("reject") } : undefined} onCancel={() => setSelected([])} />
  </div>
}

function useScrollFade(ref: React.RefObject<HTMLElement | null>, direction: "vertical" | "horizontal" = "vertical") {
  const [strength, setStrength] = useState(0)
  useEffect(() => {
    const element = ref.current
    if (!element) return
    const update = () => {
      const distance = direction === "vertical" ? element.scrollHeight - element.scrollTop - element.clientHeight : element.scrollWidth - element.scrollLeft - element.clientWidth
      setStrength(Math.min(1, Math.max(0, distance / 32)))
    }
    update()
    element.addEventListener("scroll", update, { passive: true })
    const observer = new ResizeObserver(update)
    observer.observe(element)
    const mutations = new MutationObserver(update)
    mutations.observe(element, { childList: true, subtree: true })
    return () => { element.removeEventListener("scroll", update); observer.disconnect(); mutations.disconnect() }
  }, [ref, direction])
  return strength
}

type SectionIndicatorItem = { id: string; label: string }

function SectionIndicator({ items, scrollRef }: { items: SectionIndicatorItem[]; scrollRef: React.RefObject<HTMLElement | null> }) {
  const [activeId, setActiveId] = useState(items[0]?.id ?? "")
  const [scrollable, setScrollable] = useState(false)
  useEffect(() => {
    const root = scrollRef.current
    if (!root) return
    const update = () => {
      setScrollable(root.scrollHeight > root.clientHeight + 4)
      const atBottom = root.scrollTop >= root.scrollHeight - root.clientHeight - 2
      if (atBottom) {
        setActiveId(items.at(-1)?.id ?? "")
        return
      }
      const rootTop = root.getBoundingClientRect().top
      const marker = rootTop + Math.min(180, root.clientHeight * 0.3)
      let current = items[0]?.id ?? ""
      for (const item of items) {
        const element = root.querySelector<HTMLElement>(`[data-section-id="${item.id}"]`)
        if (element && element.getBoundingClientRect().top <= marker) current = item.id
      }
      setActiveId(current)
    }
    update()
    root.addEventListener("scroll", update, { passive: true })
    const observer = new ResizeObserver(update)
    observer.observe(root)
    return () => { root.removeEventListener("scroll", update); observer.disconnect() }
  }, [items, scrollRef])
  if (!scrollable || items.length < 2) return null
  const activeIndex = Math.max(0, items.findIndex((item) => item.id === activeId))
  const start = Math.max(0, Math.min(items.length - 12, activeIndex - 5))
  const visibleItems = items.slice(start, start + 12)
  const jump = (id: string) => {
    const root = scrollRef.current
    const target = root?.querySelector<HTMLElement>(`[data-section-id="${id}"]`)
    if (!root || !target) return
    const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches
    root.scrollTo({ top: root.scrollTop + target.getBoundingClientRect().top - root.getBoundingClientRect().top - 24, behavior: reduceMotion ? "auto" : "smooth" })
  }
  return <nav aria-label="Seitenabschnitte" className="fixed right-4 top-1/2 z-40 flex translate-x-[8%] -translate-y-1/2 flex-col items-end rounded-md px-2 py-3 max-[890px]:hidden">
    {visibleItems.map((item) => <Tooltip key={item.id}><TooltipTrigger render={<button aria-label={`Zu ${item.label}`} onClick={() => jump(item.id)} className="group flex h-3 w-10 items-center justify-end" />}><span className={`h-[2px] rounded-full bg-white transition-[width,opacity] duration-200 ${activeId === item.id ? "w-5 opacity-55" : "w-3 opacity-25 group-hover:w-[18px] group-hover:opacity-45"}`} /></TooltipTrigger><TooltipContent side="left" sideOffset={10}>{item.label}</TooltipContent></Tooltip>)}
  </nav>
}

function ScrollFade({ strength, direction = "vertical", compact = false, targetRef }: { strength: number; direction?: "vertical" | "horizontal"; compact?: boolean; targetRef: React.RefObject<HTMLElement | null> }) {
  const scrollForward = () => {
    const element = targetRef.current
    if (!element) return
    if (direction === "horizontal") element.scrollBy({ left: Math.max(280, element.clientWidth * 0.65), behavior: "smooth" })
    else element.scrollBy({ top: Math.max(240, element.clientHeight * 0.65), behavior: "smooth" })
  }
  if (direction === "horizontal") return <div className={`pointer-events-none absolute bottom-0 right-0 top-0 z-10 flex origin-right items-center bg-gradient-to-l from-[#171717] via-[#171717]/80 to-transparent transition-[opacity,transform] duration-200 ${compact ? "pl-8 pr-1" : "pl-14 pr-3"}`} style={{ opacity: strength, transform: `scaleX(${0.55 + strength * 0.45})` }}><button type="button" tabIndex={strength > 0 ? 0 : -1} aria-label="Weiter nach rechts scrollen" onClick={scrollForward} className="pointer-events-auto flex h-11 w-[25px] items-center justify-center rounded-full border border-white/10 bg-[#232323]/90 backdrop-blur transition-colors hover:border-white/20 hover:bg-[#2b2b2b]"><ChevronRight className="size-3.5 text-[#777]" /></button></div>
  return <div className="pointer-events-none absolute -bottom-[17px] inset-x-0 z-20 flex justify-center bg-[linear-gradient(to_top,rgba(23,23,23,1)_0%,rgba(23,23,23,0.98)_22%,rgba(23,23,23,0.78)_52%,rgba(23,23,23,0.28)_78%,transparent_100%)] pb-7 pt-24 transition-opacity duration-200" style={{ opacity: strength }}><button type="button" tabIndex={strength > 0 ? 0 : -1} aria-label="Weiter nach unten scrollen" onClick={scrollForward} className="pointer-events-auto flex h-[25px] w-11 items-center justify-center rounded-full border border-white/10 bg-[#232323]/90 backdrop-blur transition-colors hover:border-white/20 hover:bg-[#2b2b2b]"><ChevronDown className="size-3.5 text-[#777]" /></button></div>
}

function FloatingActions({ visible, primary, onPrimary, onCancel, disabled = false, middle }: { visible: boolean; primary: string; onPrimary: () => void; onCancel: () => void; disabled?: boolean; middle?: { label: string; onClick: () => void } }) {
  const [mounted, setMounted] = useState(visible)
  useEffect(() => {
    if (visible) { setMounted(true); return }
    if (!mounted) return
    const timeout = window.setTimeout(() => setMounted(false), 500)
    return () => window.clearTimeout(timeout)
  }, [visible, mounted])
  if (!mounted) return null
  // Positionierung im äußeren Wrapper, damit sich `absolute` nicht mit dem
  // `relative` des SegmentedControl-Roots beißt (Tailwind-stylesheet-Reihenfolge
  // würde sonst `relative` gewinnen lassen und die Leiste auf volle Breite ziehen).
  const options = [
    { value: "primary" as const, label: primary, icon: Check, disabled },
    ...(middle ? [{ value: "middle" as const, label: middle.label, icon: ShieldCheck }] : []),
    { value: "cancel" as const, label: "Abbrechen", icon: X },
  ]
  // WICHTIG: keine -translate-x-1/2-Klasse hier! Tailwind v4 setzt translate
  // als eigene CSS-Property, die sich mit dem translate(-50%) der Enter/Exit-
  // Animation ADDIEREN würde (-> doppelter Versatz, Leiste hängt links raus).
  // Die Zentrierung kommt ausschließlich aus den Keyframes; für Reduced-
  // Motion stellt die CSS-Unten den Transform explizit bereit.
  return <div className={`${visible ? "floating-action-enter" : "floating-action-exit"} fixed bottom-8 left-1/2 z-40`}>
    <SegmentedControl
      className="shadow-[0_30px_60px_rgba(0,0,0,.45)]"
      buttonClassName="px-4 text-sm"
      ariaLabel={primary}
      options={options}
      value="primary"
      onChange={(next) => { if (next === "cancel") onCancel(); else if (next === "middle") middle?.onClick(); else if (!disabled) onPrimary() }}
    />
  </div>
}

// Segmentiertes Steuerelement mit gleitender weißer Pille: Der Indikator folgt
// der hovered Option und gleitet beim Klicken oder Verlassen an die aktive
// Position zurück. Die Buttons behalten ihre natürliche Inhaltsbreite und der
// dunkle Track (#171717) hinter ihnen bleibt wie vorher erhalten.
type SegmentOption<T extends string> = { value: T; label: string; icon?: typeof Bell; disabled?: boolean }

function SegmentedControl<T extends string>({ options, value, onChange, size = "lg", className = "", buttonClassName, ariaLabel }: { options: SegmentOption<T>[]; value: T; onChange: (value: T) => void; size?: "lg" | "sm"; className?: string; buttonClassName?: string; ariaLabel?: string }) {
  const [hoverIndex, setHoverIndex] = useState<number | null>(null)
  const activeIndex = Math.max(0, options.findIndex((option) => option.value === value))
  const pillIndex = hoverIndex ?? activeIndex
  const pad = size === "sm" ? 4 : 6
  // Pille per Messung positionieren: gleiche Position/Größe wie der jeweilige
  // Button (natürliche Breite), animiert über left/width mit Transition.
  const buttonRefs = useRef<(HTMLButtonElement | null)[]>([])
  const [pill, setPill] = useState<{ left: number; width: number } | null>(null)
  // Segmentgrenzen für die kurzen Trennlinien (gleiches Design wie die
  // Zellentrennlinien der Tabelle); nur bei mehr als zwei Optionen.
  const [edges, setEdges] = useState<number[]>([])
  useLayoutEffect(() => {
    const el = buttonRefs.current[pillIndex]
    if (!el) return
    const left = el.offsetLeft
    const width = el.offsetWidth
    setPill((prev) => (prev && prev.left === left && prev.width === width ? prev : { left, width }))
    if (options.length > 2) {
      const next: number[] = []
      for (let index = 1; index < options.length; index++) {
        const button = buttonRefs.current[index]
        if (button) next.push(button.offsetLeft)
      }
      setEdges((prev) => (prev.length === next.length && prev.every((value, index) => value === next[index]) ? prev : next))
    } else if (edges.length > 0) {
      setEdges([])
    }
  })
  return <div role="group" aria-label={ariaLabel} onMouseLeave={() => setHoverIndex(null)} className={`relative flex items-center rounded-md border border-white/10 bg-[#232323] ${size === "sm" ? "h-8 p-1" : "h-[52px] p-1.5"} ${className}`}>
    {/* Dunkler Track hinter den Buttons – wie vorher Teil des Designs. */}
    <span aria-hidden className="pointer-events-none absolute bg-[#171717]" style={{ top: pad, bottom: pad, left: pad, right: pad, borderRadius: size === "sm" ? 2 : 4 }} />
    {/* Kurze Trennlinien an den Segmentgrenzen – nur zwischen nicht
        ausgewählten Segmenten; neben/unter der Pille verschwinden sie. */}
    {edges.map((left, index) => (index === pillIndex || index + 1 === pillIndex) ? null : <span key={index} aria-hidden className="pointer-events-none absolute top-1/2 h-4 w-px -translate-y-1/2 bg-white/10" style={{ left }} />)}
    {pill && <span aria-hidden className={`pointer-events-none absolute bg-white transition-all duration-200 ease-out motion-reduce:transition-none ${size === "sm" ? "rounded-sm" : "rounded"}`} style={{ top: pad, bottom: pad, left: pill.left, width: pill.width }} />}
    {options.map((option, index) => <button key={option.value} ref={(el) => { buttonRefs.current[index] = el }} type="button" disabled={option.disabled} aria-pressed={option.value === value} onClick={() => onChange(option.value)} onMouseEnter={() => setHoverIndex(index)} onFocus={() => setHoverIndex(index)} className={`relative z-10 flex h-full items-center gap-2 whitespace-nowrap transition-colors duration-200 motion-reduce:transition-none ${size === "sm" ? "rounded-sm px-2 text-[10px] font-medium" : buttonClassName ?? "px-3.5 text-sm"} ${pillIndex === index ? "text-[#171717]" : size === "sm" ? "text-[#666]" : "text-[#a8a8a8]"} disabled:cursor-not-allowed disabled:opacity-40`}>{option.label}{option.icon && <option.icon className={size === "sm" ? "size-3" : "size-4"} />}</button>)}
  </div>
}

function ViewSwitch({ value, onChange }: { value: "review" | "spam"; onChange: (value: "review" | "spam") => void }) {
  return <SegmentedControl ariaLabel="Ansicht wechseln" options={[{ value: "review", label: "Review", icon: Eye }, { value: "spam", label: "Spam", icon: Trash2 }]} value={value} onChange={onChange} />
}

function ToolbarButton({ children, iconOnly = false, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & { iconOnly?: boolean }) {
  return <button {...props} className={`flex h-[52px] shrink-0 items-center justify-center gap-2 rounded-md border border-white/10 bg-white/[0.05] text-[14px] text-[#a8a8a8] outline-none transition-colors hover:bg-white/[0.08] hover:text-white focus-visible:border-white/20 ${iconOnly ? "w-[52px]" : "px-3.5"}`}>{children}</button>
}

function ToolbarConnector() { return <span aria-hidden className="h-px w-2 shrink-0 bg-white/10" /> }

function FilterToolbarButton({ active, open, count, onToggle, onReset }: { active: boolean; open: boolean; count: number; onToggle: () => void; onReset: () => void }) {
  return <div className={`flex h-[52px] shrink-0 items-center rounded-md border transition-colors ${active ? "border-white bg-white text-[#171717]" : "border-white/10 bg-white/[0.05] text-[#a8a8a8] hover:bg-white/[0.08] hover:text-white"}`}>
    <button className="h-full px-3.5 text-[14px]" onClick={onToggle} aria-expanded={open}>Filter <span className="inline-block min-w-[92px] text-left tabular-nums opacity-70">({count} {count === 1 ? "Mail" : "Mails"})</span></button>
    <button className={`mr-1 flex size-9 items-center justify-center rounded-md transition-colors ${active ? "hover:bg-black/10" : "hover:bg-white/[0.07] hover:text-white"}`} onClick={active ? onReset : onToggle} aria-label={active ? "Filter zurücksetzen" : "Filter öffnen"}>{active ? <RotateCcw className="size-4" /> : <ListFilter className="size-4" />}</button>
  </div>
}

function SortableHead({ label, name, sort, setSort, icon: HeadIcon, last = false }: { label: string; name: SortKey; sort: { key: SortKey; direction: SortDirection }; setSort: (value: { key: SortKey; direction: SortDirection }) => void; icon: typeof Mail; last?: boolean }) {
  const active = sort.key === name && sort.direction !== null
  const Icon = !active ? ArrowUpDown : sort.direction === "asc" ? ArrowUp : ArrowDown
  // Klick-Zyklus direkt auf dem Spaltenkopf: unsortiert → aufsteigend →
  // absteigend → zurücksetzen. Das Dropdown ist damit überflüssig.
  const cycle = () => {
    if (!active) setSort({ key: name, direction: "asc" })
    else if (sort.direction === "asc") setSort({ key: name, direction: "desc" })
    else setSort({ key: name, direction: null })
  }
  return <div role="columnheader" aria-sort={active ? (sort.direction === "asc" ? "ascending" : "descending") : "none"} className={`flex h-full items-center px-4 ${last ? "" : shortDivider}`}><button type="button" onClick={cycle} aria-label={`${label} sortieren`} className={`flex w-full items-center justify-between gap-2 text-xs outline-none transition-colors focus-visible:text-white ${active ? "text-white" : "text-[#a8a8a8] hover:text-white"}`}><span className="flex min-w-0 items-center gap-2"><HeadIcon className="size-3.5 shrink-0" /><span className="truncate">{label}</span></span><Icon className="size-3.5 shrink-0" /></button></div>
}

// Sortierung pro Ansicht persistiert lesen (Seitenwechsel überlebt).
function readStoredSort(view: string): { key: SortKey; direction: SortDirection } {
  try {
    const raw = localStorage.getItem(`mailmune.sort.${view}`)
    if (raw) {
      const parsed = JSON.parse(raw) as { key: SortKey; direction: SortDirection }
      if (parsed && typeof parsed.key === "string") return { key: parsed.key, direction: parsed.direction ?? null }
    }
  } catch {
    // kaputter Eintrag: Standard benutzen
  }
  return { key: "receivedAt", direction: "desc" }
}

function StatusBadge({ status }: { status: Decision["status"] }) { const labels = { pending: "Review", moved: "Review", confirmed: "Bestätigt", rejected: "Fehlalarm", deferred: "Später" }; return <Badge variant="outline" className="border-white/10 bg-white/[0.025] text-[#aaa]">{labels[status]}</Badge> }

function Notifications({ scrollRef, items = notifications, archive, onArchiveChange, onReview, activeAccountId, accountName }: { scrollRef: React.RefObject<HTMLElement | null>; items?: NotificationItem[]; archive: Record<string, number>; onArchiveChange: (archive: Record<string, number>) => void; onReview?: () => void; activeAccountId?: string | null; accountName?: (id: string) => string }) {
  const [view, setView] = useState<"open" | "archived">("open")
  const [query, setQuery] = useState("")
  const [kind, setKind] = useState<NotificationKind | "all">("all")
  const [selected, setSelected] = useState<string[]>([])
  const visible = items
    .filter((item) => view === "archived" ? Boolean(archive[item.id]) : !archive[item.id])
    .filter((item) => kind === "all" || item.kind === kind)
    .filter((item) => { const q = query.trim().toLowerCase(); return q === "" || item.title.toLowerCase().includes(q) || item.detail.toLowerCase().includes(q) })
  const notificationSections = visible.map((item) => ({ id: `notification-${item.id}`, label: item.title }))
  const allSelected = visible.length > 0 && selected.length === visible.length
  // Massenaktion: im offenen Blick archivieren, im Archiv wiederherstellen.
  const bulkAction = () => {
    const next = { ...archive }
    for (const id of selected) { if (view === "open") next[id] = Date.now(); else delete next[id] }
    onArchiveChange(next)
    setSelected([])
  }
  return <div className="relative">
    <div className="flex items-start justify-between gap-6"><h1 className="pt-1 text-2xl font-medium tracking-tight">Benachrichtigungen</h1><SegmentedControl ariaLabel="Benachrichtigungsansicht" options={[{ value: "open", label: "Offen", icon: Bell }, { value: "archived", label: "Archiviert", icon: Archive }]} value={view} onChange={(next) => { setView(next); setSelected([]) }} /></div>
        {/* Toolbar: Suche + Benachrichtigungsart – gleicher Stil und Position wie bei der Zuordnungstabelle */}
        <div className="mt-8 flex max-w-4xl items-center gap-2">
          <div className="relative h-[52px] w-[300px] min-w-[220px] shrink-0">
            <Input value={query} onChange={(event) => { setQuery(event.target.value); setSelected([]) }} placeholder="Durchsuchen" className="h-full rounded-md border-white/10 bg-white/[0.05] px-3.5 pr-11 text-sm placeholder:text-[#888]" />
            <Search className="pointer-events-none absolute right-3.5 top-1/2 size-4 -translate-y-1/2 text-[#888]" />
          </div>
          <Select value={kind} onValueChange={(value) => { setKind(value as NotificationKind | "all"); setSelected([]) }}>
            <SelectTrigger className={`h-[52px]! min-w-[168px] shrink-0 rounded-md px-3.5 text-sm! ${kind !== "all" ? "border-white! bg-white! text-[#171717]! hover:bg-white/90! [&_svg]:text-[#171717]!" : "border-white/10 bg-white/[0.05] text-[#aaa]"}`}><SelectValue>{notificationKindLabels[kind]}</SelectValue></SelectTrigger>
            <SelectContent>{(["all", "scan", "review", "schedule", "error", "model"] as const).map((value) => <SelectItem key={value} value={value}>{notificationKindLabels[value]}</SelectItem>)}</SelectContent>
          </Select>
        </div>
    <div className="mt-4 max-w-4xl">
    <div className="flex items-center gap-4 border-y border-white/[0.09] py-3">
      <Checkbox checked={allSelected} onCheckedChange={() => setSelected(allSelected ? [] : visible.map((item) => item.id))} aria-label="Alle auswählen" />
      <span className="text-xs text-[#888]">{visible.length} {view === "open" ? "offene" : "archivierte"}{selected.length > 0 ? ` · ${selected.length} ausgewählt` : ""}</span>
    </div>
    <div className="border-b border-white/[0.09]">{visible.map((item) => { const isSelected = selected.includes(item.id); return <div key={item.id} data-section-id={`notification-${item.id}`} className={`flex items-center gap-4 border-b border-white/[0.09] py-5 last:border-b-0 ${isSelected ? "bg-white/[0.03]" : ""}`}>
      <Checkbox checked={isSelected} onCheckedChange={(checked) => setSelected((current) => checked ? [...current, item.id] : current.filter((id) => id !== item.id))} aria-label={`${item.title} auswählen`} />
      <div className="min-w-0 flex-1"><p className="flex items-center gap-2 text-sm font-medium"><span className="truncate">{item.title}</span>{item.action && <span className="size-1.5 shrink-0 rounded-full bg-[#ff6b2c]" />}{item.account && item.account !== activeAccountId && <span className="shrink-0 rounded-full border border-white/10 bg-white/[0.06] px-2 py-0.5 text-[10px] font-normal text-[#aaa]">{accountName ? accountName(item.account) : item.account}</span>}</p><p className="mt-1 text-xs leading-5 text-[#777]">{item.detail}</p><p className="mt-2 text-[11px] text-[#555]">{item.time}</p></div>
      {item.action && view === "open" && <Button size="sm" variant="outline" onClick={onReview}>Prüfen</Button>}
      <button className="flex size-9 shrink-0 items-center justify-center rounded-md text-[#777] transition-colors hover:bg-white/[0.05] hover:text-white" aria-label={view === "open" ? "Benachrichtigung archivieren" : "Benachrichtigung wiederherstellen"} onClick={() => { const next = { ...archive }; if (view === "open") next[item.id] = Date.now(); else delete next[item.id]; onArchiveChange(next) }}>{view === "open" ? <X className="size-4" /> : <ArchiveRestore className="size-4" />}</button>
    </div> })}{visible.length === 0 && <p className="py-12 text-center text-sm text-[#666]">{view === "open" ? "Keine offenen Benachrichtigungen." : "Keine archivierten Benachrichtigungen."}</p>}</div>
    {view === "archived" && <p className="mt-3 text-right text-[11px] text-[#555]">Archivierte Einträge werden nach 180 Tagen entfernt.</p>}
    </div>
    {/* Massenaktions-Leiste: wie überall in der App global fixiert unten
        (FloatingActions ist fixed) – kein Seiten-spezifischer Wrapper. */}
    <FloatingActions visible={selected.length > 0} primary={view === "open" ? `Archivieren (${selected.length})` : `Wiederherstellen (${selected.length})`} onPrimary={bulkAction} onCancel={() => setSelected([])} />
    <SectionIndicator items={notificationSections} scrollRef={scrollRef} />
  </div>
}

function SettingsPage({ accounts, refresh, activeAccountId }: { accounts: Account[]; refresh: () => void; activeAccountId: string | null }) {
  const settingsScrollRef = useRef<HTMLDivElement>(null)
  const settingsFade = useScrollFade(settingsScrollRef)
  const { theme, setTheme } = useTheme()
  // Aktives Konto: zentral in der App gewählt (Account-Switcher in der Nav).
  // Bewusst GANZ OBEN definiert, damit kein Hook/Effect darunter in eine
  // Temporal Dead Zone greifen kann (hatte die Seite zweimal gecrasht).
  const activeAccount = accounts.find((item) => item.id === activeAccountId) ?? accounts[0]
  const [mode, setMode] = useState<SafetyMode>(accounts[0]?.safetyMode ?? "safe")
  const [folderName, setFolderName] = useState(accounts[0]?.spamFolder ?? "AI_SPAM_FILTER")
  const [folderDraft, setFolderDraft] = useState(folderName)
  const [editingFolder, setEditingFolder] = useState(false)
  // Schwellen liegen PRO Postfach (Profil), nicht global pro Gerät.
  const [notificationThreshold, setNotificationThreshold] = useState(90)
  const [automaticThreshold, setAutomaticThreshold] = useState(90)
  const [fakeAccountVisible, setFakeAccountVisible] = useState(true)
  const [connectionEnabled, setConnectionEnabled] = useState<Record<string, boolean>>({ "demo-strato": true })
  const [weeklyReviewEnabled, setWeeklyReviewEnabled] = useState(true)
  const [incomingReviewEnabled, setIncomingReviewEnabled] = useState(true)
  const [deepScanEditorOpen, setDeepScanEditorOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<{ kind: "account" | "model"; id: string; label: string } | null>(null)
  // Postfach-Einstellungen: Zahnrad auf der Verbindungskarte öffnet den Dialog.
  const [settingsAccount, setSettingsAccount] = useState<Account | null>(null)
  // „Bei Posteingang“ = account.enabled serverseitig: pro Profil getrennt und
  // über Neustarts persistent statt nur lokaler Schalterzustand.
  // WICHTIG: erst NACH der activeAccount-Deklaration (Dependency-Arrays würden
  // sonst in die Temporal Dead Zone greifen und die Seite crashen).
  useEffect(() => { setIncomingReviewEnabled(activeAccount?.enabled ?? true) }, [activeAccount?.id, activeAccount?.enabled])
  const toggleIncomingReview = async (enabled: boolean) => {
    setIncomingReviewEnabled(enabled)
    const account = activeAccount
    if (!account) return
    try {
      await agentRequest("POST", "/v1/accounts", { account: { ...account, enabled } })
      await refresh()
    } catch (cause) {
      setIncomingReviewEnabled(!enabled)
      showToast(cause instanceof Error ? cause.message : String(cause), "error")
    }
  }
  // Schwellen pro Profil laden (mit Fallback auf den alten globalen Wert).
  useEffect(() => {
    const id = activeAccount?.id ?? "global"
    setNotificationThreshold(Number(readStoredValue(`mailmune.notificationThreshold.${id}`, `spamalytic.notificationThreshold.${id}`, readStoredValue("mailmune.notificationThreshold", "spamalytic.notificationThreshold", "90"))))
    setAutomaticThreshold(Number(readStoredValue(`mailmune.automaticSpamThreshold.v3.${id}`, `spamalytic.automaticSpamThreshold.v3.${id}`, readStoredValue("mailmune.automaticSpamThreshold.v2", "spamalytic.automaticSpamThreshold.v2", "90"))))
  }, [activeAccount?.id])
  const storeAutomaticThreshold = (value: number) => {
    setAutomaticThreshold(value)
    localStorage.setItem(`mailmune.automaticSpamThreshold.v3.${activeAccount?.id ?? "global"}`, String(value))
  }
  const storeNotificationThreshold = (value: number) => {
    setNotificationThreshold(value)
    localStorage.setItem(`mailmune.notificationThreshold.${activeAccount?.id ?? "global"}`, String(value))
  }
  // Mail-Verbindungsstatus des aktiven Profils für das Globe-Icon: echter
  // IMAP-Login über den Test-Endpoint (nicht Scan-Historie – ein frisches
  // zweites Postfach hat noch keine Scans, ist aber verbunden).
  // Beim Profilwechsel sofort und danach alle 120 s geprüft.
  // WICHTIG: erst NACH der activeAccount-Deklaration nutzen.
  const [mailOk, setMailOk] = useState<boolean | null>(null)
  useEffect(() => {
    if (!isTauri() || !activeAccount) { setMailOk(null); return }
    let cancelled = false
    const check = async () => {
      try {
        await agentRequest<{ supportsIdle: boolean; supportsMove: boolean; folders: string[] }>("POST", `/v1/accounts/${activeAccount.id}/test`)
        if (!cancelled) setMailOk(true)
      } catch {
        if (!cancelled) setMailOk(false)
      }
    }
    void check()
    const interval = window.setInterval(() => void check(), 120000)
    return () => { cancelled = true; window.clearInterval(interval) }
  }, [activeAccount?.id])
  // Der Slider- und Ordnerzustand wird per useState nur einmal beim Mounten
  // gelesen. Konten laden aber asynchron und werden nach dem Speichern
  // aktualisiert; ohne diese Synchronisierung zeigt die UI weiter den
  // Standardwert, obwohl der Modus serverseitig korrekt gespeichert ist – das
  // wirkt, als ob die Einstellung nicht übernommen wurde.
  useEffect(() => {
    if (!activeAccount) return
    setMode(activeAccount.safetyMode ?? "safe")
    setFolderName(activeAccount.spamFolder ?? "AI_SPAM_FILTER")
  }, [activeAccount?.id, activeAccount?.safetyMode, activeAccount?.spamFolder])
  const runAccountAction = async (account: Account, action: "test" | "scan" | "resync") => {
    // Pill nur, wenn die Aktion ein anderes als das aktive Profil betrifft.
    const label = account.id !== activeAccountId ? account.name : undefined
    try {
      if (action === "test") {
        showToast("Verbindung wird geprüft …", "info", label)
        const result = await agentRequest<{ supportsIdle: boolean; supportsMove: boolean; folders: string[] }>("POST", `/v1/accounts/${account.id}/test`)
        showToast(`Verbunden · IDLE ${result.supportsIdle ? "verfügbar" : "nicht verfügbar"} · MOVE ${result.supportsMove ? "verfügbar" : "nicht verfügbar"}`, "info", label)
      } else {
        showToast(action === "resync" ? "Postfach wird komplett neu geprüft …" : "Prüfung wird gestartet …", "info", label)
        await startScan(account.id, action === "resync")
        // Live-Fortschritt kommt über den Eventstream im globalen ScanToast;
        // hier nur die Daten einmal nachziehen.
        await refresh()
      }
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "error", label)
    }
  }
  const persistSafetyMode = async (nextMode: SafetyMode) => {
    const account = activeAccount
    setMode(nextMode)
    if (!account) return
    try {
      // Das Spamverhalten IST die Automatik: „Manuell“ = Trockenlauf (nie
      // verschieben), „Standard“/„Autonom“ = automatische Verschiebung an.
      // Der Server verlangt weiterhin bewiesene Kalibrierung, bevor wirklich
      // verschoben werden darf.
      await agentRequest("POST", "/v1/accounts", { account: { ...account, safetyMode: nextMode, dryRun: nextMode === "confirm_all" } })
      await refresh()
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      showToast(message.includes("automation requires")
        ? "Automatik braucht Kalibrierung: mindestens 20 bestätigte Reviews mit 99,5 % Präzision. Bis dahin bleibt der Trockenlauf aktiv."
        : message || "Sicherheitsmodus konnte nicht gespeichert werden", "error")
      setMode(account.safetyMode ?? "safe")
    }
  }
  // Wochenprüfung = wöchentlicher KI-Tiefscan des Agenten. Der Zeitplan liegt
  // serverseitig am Konto; ohne verbundenes Postfach (Browser-Demo) bleibt der
  // Schalter lokal. Standard bei Aktivierung: Freitag 16:00 Uhr.
  const weekdayNames = ["Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"]
  const deepScanWeekday = (activeAccount?.deepScanWeekday ?? -1) >= 0 ? activeAccount!.deepScanWeekday! : 5
  const deepScanHour = (activeAccount?.deepScanHour ?? -1) >= 0 ? activeAccount!.deepScanHour! : 16
  const deepScanEnabled = activeAccount ? Boolean(activeAccount.deepScan) : weeklyReviewEnabled
  const deepScanDetail = activeAccount?.deepScan
    ? `${weekdayNames[deepScanWeekday]}s, ${String(deepScanHour).padStart(2, "0")}:00 Uhr · KI prüft alle Mails seit der letzten Wochenprüfung`
    : "Wöchentlich alle Mails seit der letzten Prüfung mit KI"
  const persistDeepScan = async (patch: Partial<Account>) => {
    if (!activeAccount) {
      if (patch.deepScan !== undefined) setWeeklyReviewEnabled(patch.deepScan)
      return
    }
    try {
      await agentRequest("POST", "/v1/accounts", { account: { ...activeAccount, ...patch } })
      await refresh()
    } catch {
      // Ohne Agent bleibt der lokale Zustand erhalten.
    }
  }
  const toggleDeepScan = (enabled: boolean) => {
    // Panel öffnet sich beim Einschalten und klappt beim Ausschalten wieder zu.
    setDeepScanEditorOpen(enabled)
    void persistDeepScan({ deepScan: enabled, deepScanWeekday, deepScanHour })
  }
  const settingsSections = [
    { id: "settings-app", label: "App-Einstellungen" }, { id: "settings-filter", label: "Filterverhalten" }, { id: "settings-folder", label: "Ordner" },
    { id: "settings-scan", label: "Automatische Prüfung" }, { id: "settings-account", label: "Postfach" }, { id: "settings-model", label: "KI-Modell" }, { id: "settings-security", label: "Sicherheit" },
  ]
  return <div className="relative h-full min-h-0">
    <div ref={settingsScrollRef} className="h-full overflow-x-hidden overflow-y-auto overscroll-contain pb-28 [scrollbar-gutter:stable]">
    <section data-section-id="settings-app" className="border-b border-white/[0.09] pb-7">
      <div className="mb-5"><h2 className="text-base font-medium">App-Einstellungen</h2><p className="mt-1 text-xs text-[#666]">Gelten unabhängig vom ausgewählten Postfach auf diesem Gerät.</p></div>
      <div className="grid gap-4 md:grid-cols-2"><LanguageDropdown /><div><div className="mb-2 flex items-center gap-2"><Monitor className="size-4 text-[#777]" /><label className="text-sm">Theme</label></div><Select value={theme} onValueChange={(value) => setTheme(value as "system" | "dark" | "light")}><SelectTrigger className="h-12! w-full rounded-[10px] border-white/10 bg-[#242424] px-3.5 text-sm"><SelectValue>{theme === "system" ? "System" : theme === "dark" ? "Dunkel" : "Hell"}</SelectValue></SelectTrigger><SelectContent><SelectItem value="system">System</SelectItem><SelectItem value="dark">Dunkel</SelectItem><SelectItem value="light">Hell</SelectItem></SelectContent></Select></div></div>
    </section>
    <div className="mb-6 mt-14"><h2 className="text-base font-medium">Profilkonfiguration</h2><p className="mt-1 text-xs text-[#666]">Diese Einstellungen gelten nur für das aktuell ausgewählte Postfach.</p></div>
    <div className="grid min-w-0 gap-6 xl:grid-cols-[minmax(0,612px)_minmax(0,1fr)] xl:gap-16">
    <section data-section-id="settings-filter" className="min-w-0">
      <SafetySlider mode={mode} setMode={(nextMode) => { void persistSafetyMode(nextMode); if (nextMode === "safe" && automaticThreshold < 90) { storeAutomaticThreshold(90) } }} />
      {mode !== "confirm_all" && <div className="mt-6"><AutomaticSpamThreshold value={automaticThreshold} onChange={(value) => { storeAutomaticThreshold(value); if (value < 90) setMode("aggressive") }} /></div>}
      <div className="mt-6"><NotificationStrength value={notificationThreshold} onChange={(value) => { storeNotificationThreshold(value) }} /></div>
      <div className="my-6 border-t border-white/[0.09]" />
      <div data-section-id="settings-folder" className="space-y-3">
        <div className="flex items-center gap-2"><h2 className="text-sm font-medium">Ordnername</h2><InfoTooltip><p>Ändert den IMAP-Zielordner und aktualisiert alle zugehörigen Verknüpfungen. Vorhandene Nachrichten werden dabei nicht gelöscht.</p></InfoTooltip></div>
        {editingFolder ? <input autoFocus aria-label="Ordnername" className="h-12 w-full rounded-[10px] border border-white/20 bg-[#242424] px-3.5 text-sm text-white outline-none focus:border-white/35" value={folderDraft} onChange={(event) => setFolderDraft(event.target.value)} /> : <button onClick={() => { setFolderDraft(folderName); setEditingFolder(true) }} className="flex h-12 w-full items-center justify-between rounded-[10px] border border-white/10 bg-[#242424] px-3.5 text-sm text-white/40 transition-colors hover:border-white/20 hover:text-white/70"><span>{folderName}</span><Pencil className="size-4" /></button>}
      </div>
      <div className="my-6 border-t border-white/[0.09]" />
      <div data-section-id="settings-scan" className="border-b border-white/[0.09] pb-6"><h2 className="mb-3 text-sm font-medium">Automatische Prüfung</h2><div className="space-y-3"><ConnectionCard icon={CalendarDays} title="Wochenprüfung" detail={deepScanDetail} enabled={deepScanEnabled} onEnabled={toggleDeepScan} onSettings={() => setDeepScanEditorOpen((open) => !open)}>
        {deepScanEditorOpen && <div className="mt-4 grid grid-cols-2 gap-3 border-t border-white/[0.07] pt-4">
          <div><label className="mb-1.5 block text-xs text-[#888]">Wochentag</label><Select value={String(deepScanWeekday)} onValueChange={(value) => void persistDeepScan({ deepScanWeekday: Number(value) })}><SelectTrigger className="h-10! w-full rounded-[10px] border-white/10 bg-[#242424] px-3 text-sm"><SelectValue>{weekdayNames[deepScanWeekday]}</SelectValue></SelectTrigger><SelectContent>{weekdayNames.map((name, index) => <SelectItem key={name} value={String(index)}>{name}</SelectItem>)}</SelectContent></Select></div>
          <div><label className="mb-1.5 block text-xs text-[#888]">Uhrzeit</label><Select value={String(deepScanHour)} onValueChange={(value) => void persistDeepScan({ deepScanHour: Number(value) })}><SelectTrigger className="h-10! w-full rounded-[10px] border-white/10 bg-[#242424] px-3 text-sm"><SelectValue>{String(deepScanHour).padStart(2, "0")}:00</SelectValue></SelectTrigger><SelectContent>{Array.from({ length: 24 }, (_, hour) => <SelectItem key={hour} value={String(hour)}>{String(hour).padStart(2, "0")}:00</SelectItem>)}</SelectContent></Select></div>
          {activeAccount && !activeAccount.ollamaValidated && <p className="col-span-2 text-xs leading-5 text-[#888]">Ohne validiertes KI-Modell prüft die Wochenprüfung nur mit Regeln und Statistik – verpasste Termine holt der Agent automatisch nach.</p>}
        </div>}
      </ConnectionCard><ConnectionCard icon={MailCheck} title="Bei Posteingang" detail="Neue Nachrichten direkt prüfen" enabled={incomingReviewEnabled} onEnabled={(enabled) => void toggleIncomingReview(enabled)} onSettings={() => {}} /></div></div>
    </section>
    <section className="min-w-0 space-y-6">
      <div data-section-id="settings-account" className="border-b border-white/[0.09] pb-6"><ConnectionSection title="Postfach" count={accounts.length + (fakeAccountVisible && accounts.length === 0 ? 1 : 0)} add={<div className="flex items-center gap-1.5"><TransferPlaceholder kind="learning" accountId={activeAccountId} /><TransferPlaceholder kind="profile" accountId={activeAccountId} /><AddAccount refresh={refresh} /></div>}>
        {/* Profilkonfiguration = immer nur das AKTIVE Postfach; weitere
            Profile werden über den Switcher in der Navigation gewechselt. */}
        {activeAccount && <ConnectionCard key={activeAccount.id} icon={Inbox} title={activeAccount.name} titleSuffix={mailOk !== null ? <AiStatusIcon ok={mailOk} enabled okText="Mailbox verbunden – IMAP erreichbar" badText="Mailbox nicht verbunden – der letzte Verbindungstest ist fehlgeschlagen" /> : null} detail={activeAccount.username} enabled={connectionEnabled[activeAccount.id] ?? activeAccount.enabled} onEnabled={(enabled) => setConnectionEnabled((current) => ({ ...current, [activeAccount.id]: enabled }))} onSettings={() => setSettingsAccount(activeAccount)} onDelete={() => setDeleteTarget({ kind: "account", id: activeAccount.id, label: activeAccount.name })} />}
        {accounts.length === 0 && fakeAccountVisible && <ConnectionCard icon={Inbox} title="STRATO Postfach" detail="kontakt@fliesenbetrieb.de" enabled={connectionEnabled["demo-strato"]} onEnabled={(enabled) => setConnectionEnabled((current) => ({ ...current, "demo-strato": enabled }))} onSettings={() => {}} onDelete={() => setDeleteTarget({ kind: "account", id: "demo-strato", label: "STRATO Postfach" })} />}
        {accounts.length === 0 && (!fakeAccountVisible || accounts.length > 0) && <EmptyConnectionCard text="Noch kein Postfach verbunden." />}
      </ConnectionSection></div>
      <div data-section-id="settings-model"><ConnectionSection title="KI-Modelle" tooltip="Lokale KI-Modelle werden über Ollama verbunden. Sie bleiben auf diesem Gerät und können nach einem Fähigkeitstest für unklare E-Mails eingesetzt werden." count={accounts.filter((item) => item.ollamaValidated).length} add={<span />}>
        <ModelManager accounts={activeAccount ? [activeAccount] : []} refresh={refresh} />
      </ConnectionSection></div>
      <div data-section-id="settings-security" className="flex gap-3 border-t border-white/[0.09] pt-6"><ShieldCheck className="mt-0.5 size-4 shrink-0 text-[#999]" /><div><p className="text-sm font-medium">Sicherheit</p><p className="mt-1 text-xs leading-5 text-[#777]">Passwörter liegen im Betriebssystem-Schlüsselbund. Nachrichtentexte werden nicht dauerhaft gespeichert.</p></div></div>
    </section>
    </div></div>
    <ScrollFade strength={settingsFade} targetRef={settingsScrollRef} />
    <SectionIndicator items={settingsSections} scrollRef={settingsScrollRef} />
    {settingsAccount && <MailboxSettingsDialog account={accounts.find((item) => item.id === settingsAccount.id) ?? settingsAccount} open onOpenChange={(open) => { if (!open) setSettingsAccount(null) }} refresh={refresh} onAction={(action) => { const current = accounts.find((item) => item.id === settingsAccount.id); if (current) void runAccountAction(current, action) }} />}
    <FloatingActions visible={editingFolder} primary="Speichern" disabled={!folderDraft.trim()} onPrimary={() => { const next = folderDraft.trim(); setFolderName(next); setEditingFolder(false); const account = activeAccount; if (account && account.spamFolder !== next) { void (async () => { try { await agentRequest("POST", "/v1/accounts", { account: { ...account, spamFolder: next } }); showToast(`Spam-Ordner auf „${next}“ geändert.`); await refresh() } catch (cause) { showToast(cause instanceof Error ? cause.message : String(cause), "error"); await refresh() } })() } }} onCancel={() => { setFolderDraft(folderName); setEditingFolder(false) }} />
    <FloatingActions visible={Boolean(deleteTarget)} primary="Wirklich löschen?" onPrimary={() => { if (!deleteTarget) return; const target = deleteTarget; setDeleteTarget(null); if (target.id === "demo-strato") { setFakeAccountVisible(false); return } if (target.kind === "account") { void (async () => { try { await deleteAccount(target.id) } catch (error) { showToast(`Löschen fehlgeschlagen: ${error instanceof Error ? error.message : String(error)}`, "error") } await refresh() })() } }} onCancel={() => setDeleteTarget(null)} />
  </div>
}

function LanguageDropdown() {
  const [language, setLanguage] = useState(() => readStoredValue("mailmune-language", "spamalytic-language", "de"))
  const [query, setQuery] = useState("")
  const languages = [{ value: "de", label: "Deutsch", available: true }, { value: "en", label: "English", available: false }].filter((item) => item.label.toLowerCase().includes(query.toLowerCase()))
  const choose = (value: string) => { setLanguage(value); localStorage.setItem("mailmune-language", value) }
  return <div><div className="mb-2 flex items-center gap-2"><Globe2 className="size-4 text-[#777]" /><label className="text-sm">Sprache</label></div><DropdownMenu><DropdownMenuTrigger render={<button className="flex h-12 w-full items-center justify-between rounded-[10px] border border-white/10 bg-[#242424] px-3.5 text-sm outline-none transition-colors hover:border-white/20" />}><span>{language === "de" ? "Deutsch" : "English"}</span><ChevronsUpDown className="size-4 text-[#777]" /></DropdownMenuTrigger><DropdownMenuContent align="start" className="w-[280px] p-2"><div className="relative mb-2"><Input value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => event.stopPropagation()} placeholder="Sprache suchen" className="h-10 pr-9 text-sm" /><Search className="pointer-events-none absolute right-3 top-1/2 size-3.5 -translate-y-1/2 text-[#777]" /></div>{languages.map((item) => <DropdownMenuItem key={item.value} disabled={!item.available} onClick={() => choose(item.value)} className="justify-between">{item.label}{item.available ? language === item.value && <Check className="size-4" /> : <span className="text-[10px] text-[#666]">später</span>}</DropdownMenuItem>)}</DropdownMenuContent></DropdownMenu></div>
}

const safetyOptions = [
  { value: "confirm_all", label: "Manuell" },
  { value: "safe", label: "Standard" },
  { value: "aggressive", label: "Autonom" },
] as const

function SafetySlider({ mode, setMode }: { mode: SafetyMode; setMode: (mode: SafetyMode) => void }) {
  const index = Math.max(0, safetyOptions.findIndex((option) => option.value === mode))
  return <div className="space-y-3">
    <div className="flex items-center justify-between"><div className="flex items-center gap-2"><h2 className="text-sm font-medium">Spam Filter Verhalten</h2><InfoTooltip><div className="space-y-2"><p><strong>Manuell:</strong> Jeder Verdachtsfall muss bestätigt werden; die automatische Schwelle ist deaktiviert.</p><p><strong>Standard:</strong> Nur sehr sichere Fälle werden automatisch verschoben.</p><p><strong>Autonom:</strong> Alle Verdachtsfälle ab der festgelegten Schwelle werden verschoben.</p></div></InfoTooltip></div><span className="text-xs text-[#666]">{safetyOptions[index].label}</span></div>
    <div className="relative h-12 overflow-hidden rounded-[10px] border border-white/10 bg-[#242424] p-1">
      <div className="absolute inset-y-3 left-4 right-4 opacity-70" style={{ backgroundImage: "repeating-linear-gradient(90deg, rgba(255,255,255,.055) 0 4px, transparent 4px 14px)" }} />
      <div className="relative h-full rounded-lg bg-[#171717] transition-[width] duration-200" style={{ width: index === 0 ? "32px" : index === 1 ? "50%" : "100%" }}><span className="absolute right-3 top-1/2 h-5 w-1 -translate-y-1/2 rounded-full bg-[#242424]" /></div>
      <input aria-label="Spam Filter Verhalten" aria-valuetext={safetyOptions[index].label} className="absolute inset-0 size-full cursor-pointer opacity-0" min="0" max="2" step="1" type="range" value={index} onChange={(event) => setMode(safetyOptions[Number(event.target.value)].value)} />
    </div>
    <div className="flex justify-between text-xs text-[#666]"><span>Manuell</span><span>Autonom</span></div>
  </div>
}

const notificationThresholds = [95, 90, 85, 80] as const

const automaticSpamThresholds = [80, 85, 90, 95, 98, 99] as const

function AutomaticSpamThreshold({ value, onChange }: { value: number; onChange: (value: number) => void }) {
  const foundIndex = automaticSpamThresholds.indexOf(value as (typeof automaticSpamThresholds)[number])
  const index = foundIndex < 0 ? 2 : foundIndex
  return <div className="space-y-3">
    <div className="flex items-center justify-between"><div className="flex items-center gap-2"><h2 className="text-sm font-medium">Automatische Spam-Schwelle</h2><InfoTooltip><p>Ab diesem Score darf eine Mail automatisch als Spam gelten. Höher bedeutet weniger Fehlalarme; KI allein löst keine Aktion aus.</p></InfoTooltip></div><span className="text-xs text-[#666]">Ab {automaticSpamThresholds[index]} %</span></div>
    <div className="relative h-12 overflow-hidden rounded-[10px] border border-white/10 bg-[#242424] p-1">
      <div className="absolute inset-y-3 left-4 right-4 opacity-70" style={{ backgroundImage: "repeating-linear-gradient(90deg, rgba(255,255,255,.055) 0 4px, transparent 4px 14px)" }} />
      <div className="relative h-full rounded-lg bg-[#171717] transition-[width] duration-200" style={{ width: index === 0 ? "32px" : `${(index / (automaticSpamThresholds.length - 1)) * 100}%` }}><span className="absolute right-3 top-1/2 h-5 w-1 -translate-y-1/2 rounded-full bg-[#242424]" /></div>
      <input aria-label="Automatische Spam-Schwelle" aria-valuetext={`Ab ${automaticSpamThresholds[index]} Prozent`} className="absolute inset-0 size-full cursor-pointer opacity-0" min="0" max={automaticSpamThresholds.length - 1} step="1" type="range" value={index} onChange={(event) => onChange(automaticSpamThresholds[Number(event.target.value)])} />
    </div>
    <div className="flex justify-between text-xs text-[#666]"><span>Anfälliger</span><span>Sehr sicher</span></div>
  </div>
}

function NotificationStrength({ value, onChange }: { value: number; onChange: (value: number) => void }) {
  const index = Math.max(0, notificationThresholds.indexOf(value as (typeof notificationThresholds)[number]))
  return <div className="space-y-3">
    <div className="flex items-center justify-between"><div className="flex items-center gap-2"><h2 className="text-sm font-medium">Benachrichtigungsstärke</h2><InfoTooltip><p>Ab diesem Score erscheint eine Benachrichtigung. Eine niedrigere Schwelle erzeugt mehr Hinweise.</p></InfoTooltip></div><span className="text-xs text-[#666]">Ab {notificationThresholds[index]} %</span></div>
    <div className="relative h-12 overflow-hidden rounded-[10px] border border-white/10 bg-[#242424] p-1">
      <div className="absolute inset-y-3 left-4 right-4 opacity-70" style={{ backgroundImage: "repeating-linear-gradient(90deg, rgba(255,255,255,.055) 0 4px, transparent 4px 14px)" }} />
      <div className="relative h-full rounded-lg bg-[#171717] transition-[width] duration-200" style={{ width: index === 0 ? "32px" : `${(index / 3) * 100}%` }}><span className="absolute right-3 top-1/2 h-5 w-1 -translate-y-1/2 rounded-full bg-[#242424]" /></div>
      <input aria-label="Benachrichtigungsstärke" aria-valuetext={`Ab ${notificationThresholds[index]} Prozent`} className="absolute inset-0 size-full cursor-pointer opacity-0" min="0" max="3" step="1" type="range" value={index} onChange={(event) => onChange(notificationThresholds[Number(event.target.value)])} />
    </div>
    <div className="flex justify-between text-xs text-[#666]"><span>Wenig</span><span>Komplett</span></div>
  </div>
}

function InfoTooltip({ children }: { children: React.ReactNode }) {
  return <Tooltip><TooltipTrigger render={<button type="button" aria-label="Weitere Informationen" className="text-[#666] transition-colors hover:text-[#aaa]"><Info className="size-3.5" /></button>} /><TooltipContent side="top" className="max-w-[300px] bg-[#eeeeee] leading-5 text-[#242424]">{children}</TooltipContent></Tooltip>
}

function ConnectionSection({ title, tooltip, count, add, children }: { title: string; tooltip?: string; count: number; add: React.ReactNode; children: React.ReactNode }) {
  return <div><div className="mb-3 flex items-center justify-between"><div><div className="flex items-center gap-2"><h2 className="text-sm font-medium">{title}</h2>{tooltip && <InfoTooltip>{tooltip}</InfoTooltip>}</div><p className="mt-1 text-xs text-[#666]">{count} verbunden</p></div>{add}</div><div className="space-y-3">{children}</div></div>
}

function TransferPlaceholder({ kind, accountId }: { kind: "learning" | "profile"; accountId: string | null }) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const learning = kind === "learning"
  // Export: portables Dokument vom Agenten holen und über den nativen
  // Speichern-Dialog ablegen (Browser-Vorschau: regulärer Download).
  // Der Dialog-Pfad erweitert den fs-Scope automatisch; es braucht keine
  // dauerhafte Schreibberechtigung.
  const exportData = async () => {
    if (!accountId) return
    setBusy(true)
    try {
      const data = await exportTransfer(accountId, kind)
      const json = JSON.stringify(data, null, 2)
      const filename = `mailmune-${kind}-${new Date().toISOString().slice(0, 10)}.json`
      if (isTauri()) {
        const [{ save }, { writeTextFile }] = await Promise.all([import("@tauri-apps/plugin-dialog"), import("@tauri-apps/plugin-fs")])
        const path = await save({ defaultPath: filename, filters: [{ name: "JSON", extensions: ["json"] }] })
        if (path) {
          await writeTextFile(path, json)
          setOpen(false)
        }
      } else {
        const url = URL.createObjectURL(new Blob([json], { type: "application/json" }))
        const anchor = document.createElement("a")
        anchor.href = url
        anchor.download = filename
        anchor.click()
        URL.revokeObjectURL(url)
      }
    } catch {
      // Exportfehler still ignorieren; der Transfer bleibt ein optionales Extra.
    } finally {
      setBusy(false)
    }
  }
  return <><Tooltip><TooltipTrigger render={<Button size="sm" variant="outline" onClick={() => setOpen(true)}>{learning ? "Lerntransfer" : "Profiltransfer"}</Button>} /><TooltipContent side="top">{learning ? "Anonymisierte Lernmerkmale übertragen" : "Vollständiges Lernprofil übertragen"}</TooltipContent></Tooltip><Dialog open={open} onOpenChange={setOpen}><DialogContent className="border-white/[0.08] bg-[#1d1d1d] sm:max-w-[520px]"><DialogHeader><DialogTitle>{learning ? "Lerndaten übertragen" : "Profil übertragen"}</DialogTitle><DialogDescription>{learning ? "Überträgt ausschließlich allgemeine, anonymisierte Lernmerkmale ohne Nachrichtentexte oder personenbezogene Daten." : "Überträgt Regeln, Präferenzen und postfachspezifische Lernmerkmale in ein anderes Profil."}</DialogDescription></DialogHeader><div className="space-y-3 py-2"><Label htmlFor={`${kind}-target`}>Zielprofil</Label><Input id={`${kind}-target`} className="h-12 px-3.5" placeholder="Profile durchsuchen" /><div className="rounded-[10px] border border-dashed border-white/10 px-4 py-5 text-center text-xs text-[#666]">Die direkte Profilauswahl und Übertragung werden später angebunden. „Exportieren“ erstellt eine portable Datei für einen anderen Rechner.</div></div><DialogFooter><Button variant="outline" disabled={busy || !accountId} onClick={() => void exportData()}>{busy ? "Exportiere…" : "Exportieren"}</Button><Button variant="outline" onClick={() => setOpen(false)}>Schließen</Button></DialogFooter></DialogContent></Dialog></>
}

// Vollständige Sprachauswahl mit Suchfeld und Checkboxen im Dropdown –
// dieselbe Mehrfachauswahl-Anmutung wie die shadcn-Combobox-Patterns, aber
// mit den vorhandenen primitives gebaut (kein neues Dependency).
const allLanguages = ["Deutsch", "Englisch", "Französisch", "Spanisch", "Italienisch", "Portugiesisch", "Niederländisch", "Polnisch", "Tschechisch", "Slowakisch", "Ungarisch", "Rumänisch", "Bulgarisch", "Kroatisch", "Slowenisch", "Griechisch", "Türkisch", "Arabisch", "Hebräisch", "Russisch", "Ukrainisch", "Chinesisch", "Japanisch", "Koreanisch", "Vietnamesisch", "Thailändisch", "Indonesisch", "Hindi", "Schwedisch", "Dänisch", "Norwegisch", "Finnisch", "Estnisch", "Lettisch", "Litauisch", "Isländisch"]

function LanguageMultiSelect({ value, onChange }: { value: string[]; onChange: (next: string[]) => void }) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const rootRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const onDown = (event: MouseEvent) => { if (rootRef.current && !rootRef.current.contains(event.target as Node)) setOpen(false) }
    document.addEventListener("mousedown", onDown)
    return () => document.removeEventListener("mousedown", onDown)
  }, [open])
  const filtered = allLanguages.filter((language) => language.toLowerCase().includes(query.trim().toLowerCase()))
  const toggle = (language: string) => onChange(value.includes(language) ? value.filter((item) => item !== language) : [...value, language])
  return <div ref={rootRef} className="relative">
    <button type="button" onClick={() => setOpen((current) => !current)} aria-expanded={open} aria-haspopup="listbox" className="flex h-12 w-full items-center justify-between gap-2 rounded-md border border-white/10 bg-white/[0.05] px-3.5 text-sm text-[#ccc] transition-colors hover:bg-white/[0.08]">
      <span className="truncate">{value.length > 0 ? value.join(", ") : "Sprachen wählen"}</span>
      <ChevronDown className={`size-4 shrink-0 text-[#777] transition-transform ${open ? "rotate-180" : ""}`} />
    </button>
    {open && <div className="absolute left-0 right-0 top-[calc(100%+6px)] z-50 rounded-md border border-white/10 bg-[#232323] p-2 shadow-[0_30px_60px_rgba(0,0,0,.45)]">
      <div className="relative mb-2">
        <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Sprache suchen" className="h-10 px-3.5 pr-10 text-sm" autoFocus />
        <Search className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-[#777]" />
      </div>
      <div className="max-h-56 overflow-y-auto" role="listbox" aria-multiselectable>
        {filtered.map((language) => <label key={language} className="flex cursor-pointer items-center gap-2.5 rounded-md px-2 py-1.5 text-sm text-[#ccc] transition-colors hover:bg-white/[0.05]">
          <Checkbox checked={value.includes(language)} onCheckedChange={() => toggle(language)} aria-label={language} />
          <span className="truncate">{language}</span>
        </label>)}
        {filtered.length === 0 && <p className="px-2 py-3 text-center text-xs text-[#666]">Keine Sprache gefunden</p>}
      </div>
    </div>}
  </div>
}

// Postfach-Einstellungen: derselbe Umfang wie die Einrichtung, nachträglich
// anpassbar – organisiert in Bereichen (Verbindung / KI-Profil / Verhalten).
// Speichern ohne Passwort lässt den Schlüsselbund-Eintrag unangetastet;
// geänderte Zugangsdaten prüft der Server gegen bereits verbundene Postfächer
// (Identitätscheck host+username), damit kein Doppel-Profil entsteht.
const mailTypePresets = ["Kundenanfragen", "Lieferanten", "Newsletter", "Automatische Kontomails"]

function MailboxSettingsDialog({ account, open, onOpenChange, refresh, onAction }: { account: Account; open: boolean; onOpenChange: (open: boolean) => void; refresh: () => void; onAction: (action: "test" | "scan" | "resync") => void }) {
  const [tab, setTab] = useState("connection")
  const [draft, setDraft] = useState(account)
  const [password, setPassword] = useState("")
  const [showPassword, setShowPassword] = useState(false)
  const [saving, setSaving] = useState(false)
  // KI-Profilmodell: Kompilat-Status (aktiv/veraltet/fehlt) + Generator.
  const [profileState, setProfileState] = useState<{ model: ProfileModel | null; stale: boolean } | null>(null)
  const [compiling, setCompiling] = useState(false)
  // Tag-Eingabe für erwartete Mailtypen.
  const [mailTypeDraft, setMailTypeDraft] = useState("")
  // Lernen zurücksetzen (mit Bestätigung).
  const [resetOpen, setResetOpen] = useState(false)
  const [resetBusy, setResetBusy] = useState(false)
  // „Neu prüfen“ mit Bereichsauswahl: Alles (Standard) oder benutzerdefinierter
  // Zeitraum über denselben Datumpicker wie die Review-Tabelle.
  const [rescanOpen, setRescanOpen] = useState(false)
  const [rescanMode, setRescanMode] = useState<"all" | "custom">("all")
  const [rescanRange, setRescanRange] = useState<{ from: string; to: string } | null>(null)
  const [pickerOpen, setPickerOpen] = useState(false)
  const [rescanBusy, setRescanBusy] = useState(false)
  // Nur beim Öffnen/Profilwechsel synchronisieren: Der 15s-Poll ersetzt das
  // Account-Objekt ständig; ein Reset bei jeder Identitätsänderung würde
  // Eingaben mitten im Tippen überschreiben.
  useEffect(() => {
    if (!open) return
    setTab("connection")
    setPassword("")
    setShowPassword(false)
    setDraft({ ...account, profile: { ...account.profile, context: account.profile.context ?? "", unexpected: account.profile.unexpected ?? "" } })
  }, [open, account.id])
  useEffect(() => {
    if (!open) return
    let cancelled = false
    getProfileModel(account.id).then((result) => { if (!cancelled) setProfileState(result) }).catch(() => { if (!cancelled) setProfileState(null) })
    return () => { cancelled = true }
  }, [open, account.id])
  const runRescan = async () => {
    setRescanBusy(true)
    try {
      const since = rescanMode === "custom" && rescanRange ? `${rescanRange.from}T00:00:00Z` : undefined
      await startScan(account.id, true, since)
      showToast(since ? `Postfach wird ab ${rescanRange!.from} neu geprüft …` : "Postfach wird komplett neu geprüft …")
      setRescanOpen(false)
      await refresh()
    } catch (cause) {
      showToast(cause instanceof Error ? cause.message : String(cause), "error")
    } finally {
      setRescanBusy(false)
    }
  }
  const credentialsChanged = draft.host !== account.host || draft.port !== account.port || draft.username !== account.username
  const save = async () => {
    setSaving(true)
    try {
      await agentRequest("POST", "/v1/accounts", { account: draft, ...(password ? { password } : {}) })
      setPassword("")
      await refresh()
      onOpenChange(false)
    } catch (cause) {
      showToast(cause instanceof Error ? cause.message : String(cause), "error")
    } finally {
      setSaving(false)
    }
  }
  // Generieren speichert zuerst den Profiltext (die Kompilierung arbeitet
  // serverseitig auf dem gespeicherten Profil), dann leitet die KI daraus
  // Indikatoren und Prompt neu ab.
  const generate = async () => {
    if (!account.ollamaValidated || !account.aiEnabled) {
      showToast("KI nicht eingerichtet oder ausgeschaltet: zuerst Modell wählen, validieren und die KI einschalten.", "error")
      return
    }
    setCompiling(true)
    try {
      await agentRequest("POST", "/v1/accounts", { account: draft })
      await refresh()
      const model = await compileProfile(account.id)
      setProfileState({ model, stale: false })
    } catch (cause) {
      showToast(cause instanceof Error ? cause.message : String(cause), "error")
    } finally {
      setCompiling(false)
    }
  }
  const toggleIndicators = async (enabled: boolean) => {
    try {
      await setProfileModelEnabled(account.id, enabled)
      setProfileState((current) => (current?.model ? { ...current, model: { ...current.model, enabled } } : current))
    } catch {
      // UI-Zustand bleibt unverändert; der nächste Dialog-Aufruf lädt neu.
    }
  }
  const addMailTypeValue = (value: string) => {
    const trimmed = value.trim()
    if (!trimmed || draft.profile.expectedMailTypes.includes(trimmed)) return
    setDraft({ ...draft, profile: { ...draft.profile, expectedMailTypes: [...draft.profile.expectedMailTypes, trimmed] } })
  }
  const addMailType = () => {
    addMailTypeValue(mailTypeDraft)
    setMailTypeDraft("")
  }
  const removeMailType = (value: string) => {
    setDraft({ ...draft, profile: { ...draft.profile, expectedMailTypes: draft.profile.expectedMailTypes.filter((item) => item !== value) } })
  }
  const doResetLearning = async () => {
    setResetBusy(true)
    try {
      const result = await resetLearning(account.id)
      showToast(result.cleared > 0
        ? `Lokales Lernen zurückgesetzt: ${result.cleared} bestätigte Beispiele entfernt.`
        : "Kein gespeichertes Lernen zum Zurücksetzen vorhanden.")
    } catch (cause) {
      showToast(cause instanceof Error ? cause.message : String(cause), "error")
    } finally {
      setResetBusy(false)
      setResetOpen(false)
    }
  }
  return <><Dialog open={open} onOpenChange={onOpenChange}><DialogContent className="flex max-h-[88vh] flex-col overflow-hidden border-white/[0.08] bg-[#1d1d1d] sm:max-w-[640px]">
    <DialogHeader><DialogTitle>Postfach-Einstellungen</DialogTitle></DialogHeader>
    {/* Bereichs-Tabs: nur so breit wie ihr Inhalt; werden es mehr als in die
        Karte passen, wird der Wrapper zum Scroll-Container. */}
    <div className="overflow-x-auto py-1">
      <SegmentedControl className="w-fit" ariaLabel="Einstellungsbereiche" options={[{ value: "connection", label: "Verbindung" }, { value: "profile", label: "KI-Profil" }, { value: "behavior", label: "Verhalten" }]} value={tab} onChange={(next) => { if (next) setTab(next) }} />
    </div>
    {/* Feste Inhaltshöhe: Beim Umschalten der Bereiche darf der zentrierte
        Dialog nicht in der Höhe springen und anders landen. Auf niedrigen
        Fenstern schrumpft der Bereich (min-h-0 + shrink), damit Header,
        Tabs UND der Footer mit Abbrechen/Speichern immer sichtbar bleiben. */}
    <div className="h-[540px] min-h-0 shrink overflow-y-auto pr-1 [scrollbar-gutter:stable]">
    {tab === "connection" && <div className="space-y-5 py-2">
      <div className="grid grid-cols-2 gap-3">
        <Field label="Anzeigename"><Input className="h-12 px-3.5" value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} /></Field>
        <Field label="IMAP-Server"><Input className="h-12 px-3.5" value={draft.host} onChange={(event) => setDraft({ ...draft, host: event.target.value })} /></Field>
        <Field label="Port"><Input className="h-12 px-3.5" inputMode="numeric" value={String(draft.port)} onChange={(event) => setDraft({ ...draft, port: Number(event.target.value.replace(/\D/g, "")) || 0 })} /></Field>
        <Field label="Benutzername"><Input className="h-12 px-3.5" value={draft.username} onChange={(event) => setDraft({ ...draft, username: event.target.value })} /></Field>
      </div>
      <div className="space-y-2">
        <div className="flex items-center gap-2"><Label>Passwort</Label><InfoTooltip><p>Leer lassen = das gespeicherte Passwort bleibt unverändert. Nur bei neuen Zugangsdaten ausfüllen.</p></InfoTooltip></div>
        <div className="relative">
          <Input type={showPassword ? "text" : "password"} className="h-12 px-3.5 pr-11" autoComplete="new-password" placeholder="••••••••" value={password} onChange={(event) => setPassword(event.target.value)} />
          <button type="button" aria-label={showPassword ? "Passwort verbergen" : "Passwort anzeigen"} onClick={() => setShowPassword((value) => !value)} className="absolute right-3 top-1/2 -translate-y-1/2 text-[#777] transition-colors hover:text-white">{showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}</button>
        </div>
      </div>
      {credentialsChanged && <p className="rounded-md border border-[#e0a86c]/30 bg-[#e0a86c]/[0.06] px-3 py-2 text-xs leading-5 text-[#e0a86c]">Zugangsdaten geändert: Bitte neues Passwort eingeben. Profil, Lernen und Reviews bleiben erhalten.</p>}
      <div className="grid grid-cols-3 gap-3">
        <Field label="Posteingang"><Input className="h-12 px-3.5" value={draft.inboxFolder} onChange={(event) => setDraft({ ...draft, inboxFolder: event.target.value })} /></Field>
        <Field label="Gesendet"><Input className="h-12 px-3.5" value={draft.sentFolder} onChange={(event) => setDraft({ ...draft, sentFolder: event.target.value })} /></Field>
        <Field label="Spam-Ordner"><Input className="h-12 px-3.5" value={draft.spamFolder} onChange={(event) => setDraft({ ...draft, spamFolder: event.target.value })} /></Field>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" onClick={() => onAction("test")}>Verbindung testen</Button>
        <Button size="sm" onClick={() => onAction("scan")}>Jetzt prüfen</Button>
        <Button size="sm" variant="outline" onClick={() => setRescanOpen(true)}>Neu prüfen</Button>
      </div>
    </div>}
    {tab === "profile" && <div className="space-y-6 py-2">
      <div className="flex items-center gap-2"><p className="text-sm font-medium">Infos zum Unternehmen / Postfach</p><InfoTooltip><p>Beschreibe in ganzen Sätzen, was dieses Postfach ist und was hier eintrifft. Die KI leitet daraus AB, welche konkreten Themen, Dokumenttypen und Absenderarten erwartet werden – zum Beispiel ergibt „designt Websites" + „Kundenanfragen": Anfragen zu Websites, Briefings, Logo-Entwürfe, Druck-PDFs, CMS-Begriffe, Hosting-Rechnungen. Sie kopiert nicht nur deine Wörter.</p></InfoTooltip></div>
      <Field label="Zweck des Postfachs"><Textarea className="min-h-20 px-3.5 py-3" placeholder="Zum Beispiel: Kundenanfragen, Angebote und Rechnungen einer Design-Agentur für Websites und Logos" value={draft.profile.purpose} onChange={(event) => setDraft({ ...draft, profile: { ...draft.profile, purpose: event.target.value } })} /></Field>
      <div className="grid grid-cols-2 gap-3">
        <Field label="Branche"><Input className="h-12 px-3.5" value={draft.profile.industry} onChange={(event) => setDraft({ ...draft, profile: { ...draft.profile, industry: event.target.value } })} /></Field>
        <Field label="Erwartete Sprachen"><LanguageMultiSelect value={draft.profile.languages} onChange={(next) => setDraft({ ...draft, profile: { ...draft.profile, languages: next } })} /></Field>
      </div>
      <Field label="Welche Mails erwartest du?">
        <div className="flex gap-2">
          <Input className="h-12 flex-1 px-3.5" placeholder="Mailtyp eingeben, Enter drücken" value={mailTypeDraft} onChange={(event) => setMailTypeDraft(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") { event.preventDefault(); addMailType() } }} />
          <button type="button" aria-label="Mailtyp hinzufügen" onClick={addMailType} disabled={!mailTypeDraft.trim()} className="flex h-12 w-12 shrink-0 items-center justify-center rounded-md border border-white/10 bg-white/[0.05] text-[#a8a8a8] transition-colors hover:bg-white/[0.08] hover:text-white disabled:pointer-events-none disabled:opacity-40"><CornerDownLeft className="size-4" /></button>
        </div>
        {draft.profile.expectedMailTypes.length > 0 && <div className="mt-2 flex gap-1.5 overflow-x-auto pb-1.5">{draft.profile.expectedMailTypes.map((type) => <span key={type} className="flex shrink-0 items-center gap-1.5 rounded-md border border-white/10 bg-white/[0.04] px-2.5 py-1 text-xs text-[#ccc]">{type}<button type="button" aria-label={`${type} entfernen`} onClick={() => removeMailType(type)} className="text-[#777] transition-colors hover:text-white"><X className="size-3" /></button></span>)}</div>}
        <div className="mt-0.5 flex flex-wrap items-center gap-1.5">{mailTypePresets.filter((preset) => !draft.profile.expectedMailTypes.includes(preset)).map((preset) => <button key={preset} type="button" onClick={() => addMailTypeValue(preset)} className="flex items-center gap-1 rounded-md border border-dashed border-white/15 px-2.5 py-1 text-xs text-[#777] transition-colors hover:border-white/30 hover:text-[#ccc]"><Plus className="size-3" />{preset}</button>)}</div>
      </Field>
      <Field label="Vertrauenswürdige Absender (optional)"><Textarea className="min-h-16 px-3.5 py-3" placeholder={"Eine E-Mail-Adresse oder Domain pro Zeile\nlieferant@beispiel.de"} value={(draft.profile.trustedSenders ?? []).join("\n")} onChange={(event) => setDraft({ ...draft, profile: { ...draft.profile, trustedSenders: event.target.value.split(/[\n,;]/).map((value) => value.trim()).filter(Boolean) } })} /></Field>
      <Field label="Ungewöhnlich, aber legitim"><Textarea className="min-h-16 px-3.5 py-3" placeholder="Zum Beispiel: Newsletter von Design-Blogs, Rechnungen vom Hosting-Anbieter, Mails von Freelancern" value={draft.profile.context} onChange={(event) => setDraft({ ...draft, profile: { ...draft.profile, context: event.target.value } })} /></Field>
      <div className="space-y-2">
        <div className="flex items-center gap-2"><Label>Was erwartest du hier niemals?</Label><InfoTooltip><p>Daraus leitet die KI die profilspezifischen Fremdkampagnen ab, die der Filter ausschließen soll.</p></InfoTooltip></div>
        <Textarea className="min-h-16 px-3.5 py-3" placeholder="Zum Beispiel: Diät-Werbung, Krypto-Anlagen, Krankenkassen-Lockangebote, Potenzmittel, Kaltakquise von Agenturen" value={draft.profile.unexpected} onChange={(event) => setDraft({ ...draft, profile: { ...draft.profile, unexpected: event.target.value } })} />
      </div>
      <div className="rounded-[10px] border border-white/[0.07] bg-white/[0.02] p-3">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <p className="text-xs font-medium text-[#ccc]">KI-Indikatoren</p>
            {profileState?.model && <Badge variant="outline" className="border-white/10 bg-white/[0.025] text-[#aaa]">{profileState.stale ? "veraltet" : profileState.model.enabled ? "aktiv" : "deaktiviert"}</Badge>}
          </div>
          <div className="flex items-center gap-2">
            {profileState?.model && <CompactOnOff enabled={profileState.model.enabled} onChange={(enabled) => void toggleIndicators(enabled)} label="KI-Indikatoren" />}
            <Button size="sm" variant="outline" disabled={compiling || saving} onClick={() => void generate()}>{compiling ? "Generiere…" : profileState?.model ? "Neu generieren" : "Mit KI generieren"}</Button>
          </div>
        </div>
        {profileState?.model ? (
          <div className="mt-3 space-y-2 text-xs leading-5 text-[#888]">
            {profileState.model.indicators.notes && <p>{profileState.model.indicators.notes}</p>}
            <div className="flex flex-wrap gap-1.5">{(profileState.model.indicators.expectedTopics ?? []).map((topic) => <span key={topic} className="rounded-md border border-white/10 px-2 py-0.5 text-[#9aa]">{topic}</span>)}</div>
            {(profileState.model.indicators.unexpectedTopics ?? []).map((group) => <p key={group.name}><span className="text-[#e0a86c]">{group.name}:</span> {group.terms.join(", ")}</p>)}
            <p className="text-[#666]">Kompiliert am {new Date(profileState.model.compiledAt).toLocaleString("de-DE")} · {profileState.model.model} · liegt lokal in der Datenbank</p>
          </div>
        ) : (
          <p className="mt-2 text-xs leading-5 text-[#666]">Noch nicht kompiliert – benötigt ein validiertes KI-Modell.</p>
        )}
      </div>
    </div>}
    {tab === "behavior" && <div className="space-y-3 py-2">
      {/* Automatische Verschiebung lebt bewusst NICHT hier: Sie ist über das
          Spamverhalten/die Sicherheitsstufe in den Einstellungen pro Profil
          geregelt. Hier bleibt nur der Reset des lokalen Lernens. */}
      <div className="rounded-[10px] border border-white/[0.07] bg-white/[0.02] p-3">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2"><p className="text-sm font-medium">Lernen zurücksetzen</p><InfoTooltip><p>Entfernt die lokal gelernten Merkmale aus bestätigten Reviews für dieses Postfach. Entscheidungen und E-Mails bleiben unverändert.</p></InfoTooltip></div>
          <Button size="sm" variant="outline" disabled={resetBusy} onClick={() => setResetOpen(true)}>{resetBusy ? "Setze zurück …" : "Zurücksetzen"}</Button>
        </div>
      </div>
    </div>}
    </div>
    <DialogFooter><Button variant="outline" onClick={() => onOpenChange(false)}>Abbrechen</Button><Button disabled={saving || !draft.name.trim() || !draft.host.trim() || !draft.username.trim() || (credentialsChanged && !password)} onClick={() => void save()}>{saving ? "Speichere…" : "Speichern"}</Button></DialogFooter>
  </DialogContent></Dialog>
  <Dialog open={resetOpen} onOpenChange={setResetOpen}><DialogContent className="border-white/[0.08] bg-[#1d1d1d] sm:max-w-[420px]"><DialogHeader><DialogTitle>Lokales Lernen zurücksetzen?</DialogTitle><DialogDescription>Die gelernten Merkmale aus bestätigten Reviews dieses Postfachs werden gelöscht. Entscheidungen und E-Mails bleiben unverändert; der Filter beginnt bei null zu lernen.</DialogDescription></DialogHeader><DialogFooter><Button variant="ghost" onClick={() => setResetOpen(false)}>Abbrechen</Button><Button onClick={() => void doResetLearning()}>Zurücksetzen</Button></DialogFooter></DialogContent></Dialog>
  <Dialog open={rescanOpen} onOpenChange={setRescanOpen}><DialogContent className="border-white/[0.08] bg-[#1d1d1d] sm:max-w-[440px]"><DialogHeader><DialogTitle>Neu prüfen</DialogTitle><DialogDescription>Welcher Bereich soll neu gescannt und bewertet werden?</DialogDescription></DialogHeader>
    <div className="space-y-3 py-2">
      <div className="space-y-2">
        <Label>Bereich</Label>
        <Select value={rescanMode} onValueChange={(value) => { if (value) setRescanMode(value as "all" | "custom") }}>
          <SelectTrigger className="h-12 w-full rounded-[10px] border-white/10 bg-[#242424] px-3.5 text-sm"><SelectValue>{(value) => (value === "all" ? "Alles" : "Benutzerdefiniert")}</SelectValue></SelectTrigger>
          <SelectContent><SelectItem value="all">Alles</SelectItem><SelectItem value="custom">Benutzerdefiniert</SelectItem></SelectContent>
        </Select>
      </div>
      {rescanMode === "custom" && <button type="button" onClick={() => setPickerOpen(true)} className="flex h-12 w-full items-center justify-between rounded-md border border-white/10 bg-white/[0.05] px-3.5 text-sm text-[#ccc] transition-colors hover:bg-white/[0.08]">
        <span>{rescanRange ? `${new Date(rescanRange.from).toLocaleDateString("de-DE")} – ${new Date(rescanRange.to).toLocaleDateString("de-DE")}` : "Zeitraum wählen"}</span>
        <CalendarDays className="size-4 text-[#777]" />
      </button>}
    </div>
    <DialogFooter><Button variant="ghost" onClick={() => setRescanOpen(false)}>Abbrechen</Button><Button disabled={rescanBusy || (rescanMode === "custom" && !rescanRange)} onClick={() => void runRescan()}>{rescanBusy ? "Starte …" : "Neu berechnen"}</Button></DialogFooter>
  </DialogContent></Dialog>
  <DateRangeDialog open={pickerOpen} onOpenChange={setPickerOpen} initial={rescanRange} onApply={(picked) => setRescanRange(picked)} /></>
}

function ConnectionCard({ icon: Icon, title, titleSuffix, detail, enabled, onEnabled, onSettings, onDelete, children }: { icon: typeof Inbox; title: string; titleSuffix?: React.ReactNode; detail: string; enabled: boolean; onEnabled: (enabled: boolean) => void; onSettings: () => void; onDelete?: () => void; children?: React.ReactNode }) {
  return <div className="rounded-[10px] border border-white/10 bg-[#202020] p-4"><div className="flex items-center gap-3"><div className="flex size-9 shrink-0 items-center justify-center rounded-md bg-white/[0.04] text-[#999]"><Icon className="size-4" /></div><div className="min-w-0 flex-1"><p className="flex min-w-0 items-center gap-1.5 text-sm font-medium"><span className="truncate">{title}</span>{titleSuffix}</p><p className="mt-1 truncate text-xs text-[#666]">{detail}</p></div><div className="flex shrink-0 items-center gap-0.5"><button className="flex size-8 items-center justify-center rounded-md text-[#666] transition-colors hover:bg-white/[0.05] hover:text-white" onClick={onSettings} aria-label={`${title} verwalten`}><Settings className="size-4" /></button>{onDelete && <button className="flex size-8 items-center justify-center rounded-md text-[#666] transition-colors hover:bg-white/[0.05] hover:text-white" onClick={onDelete} aria-label={`${title} löschen`}><Trash2 className="size-4" /></button>}<CompactOnOff enabled={enabled} onChange={onEnabled} label={title} /></div></div>{children}</div>
}

function CompactOnOff({ enabled, onChange, label }: { enabled: boolean; onChange: (enabled: boolean) => void; label: string }) {
  return <SegmentedControl size="sm" className="ml-2 bg-[#171717]" ariaLabel={`${label} ein- oder ausschalten`} options={[{ value: "on", label: "An" }, { value: "off", label: "Aus" }]} value={enabled ? "on" : "off"} onChange={(next) => onChange(next === "on")} />
}

function EmptyConnectionCard({ text }: { text: string }) {
  return <div className="flex min-h-20 items-center rounded-[10px] border border-dashed border-white/10 bg-white/[0.015] px-4 text-sm text-[#666]">{text}</div>
}

// Status-Icon (Globe): gedimmter Haken bei funktionierender Verbindung,
// GlobeX bei Problemen; bei ausgeschalteter Funktion gar kein Icon. Die
// Tooltip-Texte sind je Einsatzort (Mailbox vs. KI) konfigurierbar.
function AiStatusIcon({ ok, enabled, okText = "Verbunden – die KI ist aktiv und einsatzbereit", badText = "Nicht verbunden – Ollama läuft nicht, das Modell fehlt oder ist nicht validiert" }: { ok: boolean; enabled: boolean; okText?: string; badText?: string }) {
  if (!enabled) return null
  return <Tooltip><TooltipTrigger render={<span className="flex shrink-0 items-center text-[#666]" />}>{ok ? <GlobeCheck className="size-3.5" /> : <GlobeX className="size-3.5" />}</TooltipTrigger><TooltipContent side="top">{ok ? okText : badText}</TooltipContent></Tooltip>
}

function ModelManager({ accounts, refresh }: { accounts: Account[]; refresh: () => void }) {
  const account = accounts[0]
  const [installed, setInstalled] = useState<string[]>([])
  const [selected, setSelected] = useState<string>(account?.ollamaModel ?? "")
  const [busy, setBusy] = useState(false)
  // Modellwechsel-Warnung: ersetzt ein bereits validiertes, genutztes Modell,
  // muss das erst bestätigt werden (Fähigkeitstest verfällt, Bewertungen
  // können inkonsistent werden).
  const [pendingTag, setPendingTag] = useState<string | null>(null)
  // Einstellungs-Dialog (Modell wählen / lokal installiert / validieren).
  const [settingsOpen, setSettingsOpen] = useState(false)
  // Erreichbarkeit von Ollama für die Status-Icons: null = noch unbekannt.
  const [reachable, setReachable] = useState<boolean | null>(null)
  const [toggling, setToggling] = useState(false)
  const [baseline, setBaseline] = useState<LearningBaseline | null>(null)
  const [embeddingStatus, setEmbeddingStatus] = useState<EmbeddingStatus | null>(null)
  const [recsOpen, setRecsOpen] = useState(false)

  const loadModels = async () => {
    try {
      const [inst, base] = await Promise.all([listModels(), getBaseline().catch(() => ({ baseline: null }))])
      setInstalled(inst ?? [])
      setBaseline(base.baseline ?? null)
      setReachable(true)
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "error")
      setReachable(false)
    }
  }

  useEffect(() => {
    if (!isTauri()) return
    let cancelled = false
    void (async () => {
      try {
        const [inst, base] = await Promise.all([listModels(), getBaseline().catch(() => ({ baseline: null }))])
        if (cancelled) return
        setInstalled(inst ?? [])
        setBaseline(base.baseline ?? null)
        setReachable(true)
        if (account?.id) {
          const emb = await getEmbeddingStatus(account.id).catch(() => null)
          if (!cancelled && emb) setEmbeddingStatus(emb)
        }
      } catch (error) {
        if (!cancelled) {
          showToast(`Ollama ist nicht erreichbar: ${error instanceof Error ? error.message : String(error)}`, "error")
          setReachable(false)
        }
      }
    })()
    return () => { cancelled = true }
  }, [])

  useEffect(() => {
    if (account?.ollamaModel) setSelected(account.ollamaModel)
  }, [account?.ollamaModel])

  useEffect(() => {
    if (selected) return
    // Nur noch lokal installierte Modelle anbieten – Empfehlungen leben
    // außerhalb der Auswahlliste im eigenen Dialog.
    const fallback = installed[0]
    if (fallback) setSelected(fallback)
  }, [installed, selected])

  if (!account) {
    return <EmptyConnectionCard text="Zuerst ein Postfach verbinden; das KI-Modell wird pro Postfach aktiviert." />
  }

  const applyModel = async (tag: string) => {
    setSelected(tag)
    setBusy(true)
    showToast("Modell wird übernommen …")
    try {
      const result = await setAccountModel(account.id, tag)
      if (result.redirected) {
        showToast(`Automatisch als Embedding-Modell erkannt: ${tag} wirkt im Fast-Pfad (Zentroide), nicht als generatives KI-Modell.`)
        const status = await getEmbeddingStatus(account.id).catch(() => null)
        if (status) setEmbeddingStatus(status)
      } else {
        showToast(`Übernommen: ${tag}. Jetzt den Fähigkeitstest ausführen, um es zu aktivieren.`)
      }
      await refresh()
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "error")
    } finally {
      setBusy(false)
    }
  }

  // Embedding-Modell (Fast-Pfad) getrennt vom generativen KI-Modell: kein
  // Fähigkeitstest, Wirkung über importierte Zentroide.
  const applyEmbeddingModel = async (tag: string) => {
    setBusy(true)
    try {
      await agentRequest("POST", "/v1/accounts", { account: { ...account, embeddingModel: tag } })
      showToast(tag ? `Embedding-Modell übernommen: ${tag}` : "Embedding-Fast-Pfad ausgeschaltet.")
      await refresh()
      const status = await getEmbeddingStatus(account.id).catch(() => null)
      if (status) setEmbeddingStatus(status)
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "error")
    } finally {
      setBusy(false)
    }
  }

  const choose = (tag: string) => {
    // Nur warnen, wenn ein validiertes Modell im Einsatz war und ersetzt wird.
    if (account.ollamaValidated && account.ollamaModel && account.ollamaModel !== tag) {
      setPendingTag(tag)
      return
    }
    void applyModel(tag)
  }

  const validate = async () => {
    if (!selected) return
    // Embedding-Modelle generativen Tests zu unterziehen hängt nur (Ollama
    // /api/generate scheitert oder blockiert) – sofort klar benennen.
    if (/embedding/i.test(selected)) {
      showToast("Embedding-Modelle haben keinen Fähigkeitstest: Sie wirken über den Fast-Pfad (Zentroide). Bitte unten als Embedding-Modell wählen, nicht hier.", "error")
      return
    }
    setBusy(true)
    const stickyId = showToast("Fähigkeitstest läuft … (je nach Modell 1–3 Minuten)", "info", undefined, true)
    try {
      const result = await validateAccountModel(account.id, selected)
      const invalidCases = result.report.cases.filter((item) => !item.valid)
      // Surface the real cause (e.g. "model not found" or the raw output) so a
      // failure is diagnosable instead of an opaque count.
      const firstError = invalidCases.find((item) => item.error)?.error
      showToast(result.report.passed
        ? `Fähigkeitstest bestanden: ${selected} ist aktiviert und analysiert unklare Fälle mit.`
        : `Fähigkeitstest fehlgeschlagen (${invalidCases.length} ungültige Antworten). Das Modell bleibt deaktiviert.${firstError ? ` Ursache: ${firstError}` : ""}`, result.report.passed ? "info" : "error")
      await refresh()
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "error")
    } finally {
      setBusy(false)
      dismissToast(stickyId)
      void loadModels()
    }
  }

  // KI-Hauptschalter: AN startet Ollama bei Bedarf (App-Start oder hier),
  // AUS deaktiviert die Modellnutzung komplett (nur Regeln + Lernfilter).
  const toggleAI = async (enabled: boolean) => {
    if (toggling) return
    setToggling(true)
    try {
      if (enabled) {
        showToast("Ollama wird geprüft und bei Bedarf gestartet …")
        const up = await ensureOllamaRunning()
        setReachable(up)
        if (up) await loadModels()
        if (!up) showToast("Ollama konnte nicht automatisch gestartet werden. Bitte manuell starten (ollama serve).", "error")
      }
      await agentRequest("POST", "/v1/accounts", { account: { ...account, aiEnabled: enabled } })
      await refresh()
    } catch (error) {
      // invoke() rejected mit einem reinen String (Rust-Fehlertext) – immer
      // den echten Grund zeigen, nie eine generische Floskel.
      showToast(`KI-Umschalten fehlgeschlagen: ${error instanceof Error ? error.message : String(error)}`, "error")
    } finally {
      setToggling(false)
    }
  }

  const validated = account.ollamaValidated && account.ollamaModel === selected
  const aiOk = reachable === true && Boolean(account.ollamaModel) && installed.includes(account.ollamaModel ?? "")
  // Auswahl = nur lokal installierte Modelle; Empfehlungen leben außerhalb
  // der Liste im eigenen Dialog.
  const options = installed.map((tag) => ({ tag, label: tag }))
  const subtitle = !account.aiEnabled
    ? "KI ausgeschaltet – es prüfen nur Regeln und Lernfilter"
    : validated ? "Ollama · lokal" : account.ollamaModel ? "Ollama · gewählt, nicht validiert" : "Ollama · kein Modell gewählt"

  return <div className="rounded-[10px] border border-white/10 bg-[#202020] p-4">
    <div className="flex items-center gap-3">
      <div className="flex size-9 shrink-0 items-center justify-center rounded-md bg-white/[0.04] text-[#999]"><Bot className="size-4" /></div>
      <div className="min-w-0 flex-1">
        <p className="flex min-w-0 items-center gap-1.5 truncate text-sm font-medium">
          <span className="truncate">{account.ollamaModel || selected || "Kein Modell gewählt"}</span>
          <AiStatusIcon ok={aiOk} enabled={account.aiEnabled} />
          <InfoTooltip><p>Ein KI-Ergebnis allein verschiebt niemals eine Mail. Das Modell zählt als eine Signalgruppe neben Regeln und Lernfilter und läuft nur lokal.</p></InfoTooltip>
        </p>
        <p className="mt-1 truncate text-xs text-[#666]">{subtitle}</p>
      </div>
      <div className="flex shrink-0 items-center gap-0.5">
        <button className="flex size-8 items-center justify-center rounded-md text-[#666] transition-colors hover:bg-white/[0.05] hover:text-white" onClick={() => setSettingsOpen(true)} aria-label="KI-Modell-Einstellungen"><Settings className="size-4" /></button>
        <CompactOnOff enabled={account.aiEnabled} onChange={(enabled) => void toggleAI(enabled)} label="KI-Filterung" />
      </div>
    </div>

    {/* Modell-Einstellungen: Wahl, Installation und Fähigkeitstest im Dialog,
        wie bei den Postfach-Einstellungen – die Karte bleibt aufgeräumt. */}
    <Dialog open={settingsOpen} onOpenChange={setSettingsOpen}>
      <DialogContent className="border-white/[0.08] bg-[#1d1d1d] sm:max-w-[520px]">
        <DialogHeader><DialogTitle>KI-Modell</DialogTitle><DialogDescription>Modell wählen, Installation prüfen und den Fähigkeitstest für {account.name} ausführen.</DialogDescription></DialogHeader>
        <div className="space-y-3 py-2">
          <Label htmlFor="model-select">Modell wählen <button type="button" onClick={(event) => { event.preventDefault(); event.stopPropagation(); setRecsOpen(true) }} aria-label="Empfehlungen anzeigen" className="font-normal text-[#666] transition-colors hover:text-[#999]">(Empfehlungen <Plus className="mb-0.5 inline size-3.5" />)</button></Label>
          <Select value={selected} onValueChange={(value) => { if (value) choose(value) }} disabled={busy}>
            <SelectTrigger id="model-select" className="h-12 w-full rounded-[10px] border-white/10 bg-[#242424] px-3.5 text-sm">
              <SelectValue placeholder={options.length > 0 ? "Modell wählen" : "Keine Modelle installiert"} />
            </SelectTrigger>
            <SelectContent>
              {options.map((option) => <SelectItem key={option.tag} value={option.tag}>{option.label}</SelectItem>)}
            </SelectContent>
          </Select>
          <div className="flex flex-wrap gap-2">
            <Button size="sm" variant={validated ? "ghost" : "default"} onClick={() => void validate()} disabled={busy || !selected}>{busy ? "Bitte warten …" : validated ? "Erneut validieren" : "Fähigkeitstest"}</Button>
          </div>
          <div className="space-y-2 border-t border-white/[0.07] pt-3">
            <div className="flex items-center gap-2"><Label>Embedding-Modell (Fast-Pfad)</Label><InfoTooltip><p>Embedding-Modelle (z. B. qwen3-embedding:0.6b) erzeugen Vektoren statt Text: Jede Mail wird gegen importierte Spam-/Ham-Zentroide verglichen – eine eigene Signalgruppe, schnell und ohne Generierung. Die Zentroide werden einmalig per mltool embed aus einem lokalen Korpus importiert.</p></InfoTooltip></div>
            <Select value={account.embeddingModel || "none"} onValueChange={(value) => { if (value) void applyEmbeddingModel(value === "none" ? "" : value) }} disabled={busy}>
              <SelectTrigger className="h-12 w-full rounded-[10px] border-white/10 bg-[#242424] px-3.5 text-sm"><SelectValue>{(value) => (value === "none" ? "Aus" : value)}</SelectValue></SelectTrigger>
              <SelectContent>
                <SelectItem value="none">Aus</SelectItem>
                {installed.map((tag) => <SelectItem key={`emb-${tag}`} value={tag}>{tag}</SelectItem>)}
              </SelectContent>
            </Select>
            {account.embeddingModel
              ? embeddingStatus?.ready
                ? <p className="text-xs text-[#666]">Zentroide aktiv: {embeddingStatus.spamN?.toLocaleString("de-DE")} Spam / {embeddingStatus.hamN?.toLocaleString("de-DE")} Ham · Dim {embeddingStatus.dim}</p>
                : <p className="text-xs text-[#e0a86c]">Keine Zentroide für dieses Modell importiert – der Fast-Pfad bleibt aus, bis mltool embed gelaufen ist.</p>
              : <p className="text-xs text-[#666]">Empfehlung: ollama pull qwen3-embedding:0.6b</p>}
          </div>
          {baseline && <p className="text-xs leading-5 text-[#666]">Globale Lern-Baseline aktiv: {baseline.corpusRows.toLocaleString("de-DE")} externe Mails ({baseline.license}) wirken als begrenzter Prior neben deinem bestätigten Lernen.</p>}
        </div>
        <DialogFooter><Button variant="outline" onClick={() => setSettingsOpen(false)}>Schließen</Button></DialogFooter>
      </DialogContent>
    </Dialog>

    {/* Modellwechsel-Warnung: ersetzt ein validiertes Modell, das im Einsatz war. */}
    <Dialog open={Boolean(pendingTag)} onOpenChange={(open) => { if (!open) setPendingTag(null) }}>
      <DialogContent className="border-white/[0.08] bg-[#1d1d1d] sm:max-w-[440px]">
        <DialogHeader><DialogTitle>KI-Modell wechseln?</DialogTitle><DialogDescription>„{account.ollamaModel}“ ist validiert und im Einsatz. Beim Wechsel wird die Validierung zurückgesetzt und der Fähigkeitstest muss für das neue Modell erneut laufen. Bisherige KI-Bewertungen stammen vom alten Modell – Scores können bis zum nächsten Scan inkonsistent wirken. Bestätigte Reviews und Lernwissen bleiben erhalten.</DialogDescription></DialogHeader>
        <DialogFooter className="border-t border-white/[0.09] pt-4"><Button variant="ghost" onClick={() => setPendingTag(null)}>Abbrechen</Button><Button onClick={() => { const tag = pendingTag; setPendingTag(null); if (tag) void applyModel(tag) }}>Modell wechseln</Button></DialogFooter>
      </DialogContent>
    </Dialog>

    <RecommendationsDialog open={recsOpen} onOpenChange={setRecsOpen} />
  </div>
}

// Modell-Empfehlungen (Research-Stand 2026-09-25): bewusst NICHT in der
// Auswahlliste, sondern als eigener Dialog mit Begründungen – die Liste zeigt
// nur, was lokal installiert ist.
const modelRecommendationsStand = "25.09.2026"
const modelRecommendations = [
  { name: "Qwen3.5 2B", specs: "2,7 GB · 256K Kontext · Deutsch sehr gut", verdict: "Beste Gesamtwahl", why: "Für die Spam-Klassifikation aktuell der beste Kompromiss aus Qualität, Größe, Deutsch-Support und Ollama-Kompatibilität." },
  { name: "Gemma 3 1B", specs: "815 MB · 32K Kontext · Deutsch gut", verdict: "Für schwache PCs", why: "Die beste sehr leichte Alternative; läuft mit deutlich weniger Ressourcen." },
  { name: "Granite 4 1B-H", specs: "1,6 GB · 1M Kontext · Deutsch explizit", verdict: "Sehr interessant", why: "IBM-Modell mit expliziter Unterstützung für Klassifikation und Deutsch." },
  { name: "Qwen3.5 0.8B", specs: "1,0 GB · 256K Kontext · Deutsch gut", verdict: "Überraschend schwach", why: "Fällt in mehreren Benchmarks deutlich ab und nutzt sein großes Kontextfenster nicht zuverlässig." },
]

function RecommendationsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const [openItem, setOpenItem] = useState<string | null>(null)
  return <Dialog open={open} onOpenChange={onOpenChange}><DialogContent className="border-white/[0.08] bg-[#1d1d1d] sm:max-w-[480px]"><DialogHeader><div className="flex items-center gap-2"><DialogTitle>Empfehlungen</DialogTitle><InfoTooltip><p>Stand der Einschätzung: {modelRecommendationsStand} · verglichen nach Qualität, Größe, Deutsch-Support und Ollama-Kompatibilität für Spam-Klassifikation.</p></InfoTooltip></div></DialogHeader>
    <div className="divide-y divide-white/[0.07]">
      {modelRecommendations.map((item) => <div key={item.name} className="py-2">
        <button type="button" onClick={() => setOpenItem((current) => (current === item.name ? null : item.name))} aria-expanded={openItem === item.name} className="flex w-full items-center justify-between gap-2 py-1 text-left outline-none focus-visible:text-white">
          <span className="min-w-0"><span className="block truncate text-sm text-[#ddd]">{item.name}</span><span className="block truncate text-[11px] text-[#666]">{item.specs} · {item.verdict}</span></span>
          <ChevronDown className={`size-4 shrink-0 text-[#777] transition-transform ${openItem === item.name ? "rotate-180" : ""}`} />
        </button>
        {openItem === item.name && <p className="pb-2 text-xs leading-5 text-[#888]">{item.why}</p>}
      </div>)}
    </div>
    <DialogFooter><Button variant="outline" onClick={() => onOpenChange(false)}>Schließen</Button></DialogFooter>
  </DialogContent></Dialog>
}

function AddAccount({ refresh, onCreated, open: controlledOpen, onOpenChange, hideTrigger }: { refresh: () => void; onCreated?: (id: string) => void; open?: boolean; onOpenChange: (open: boolean) => void; hideTrigger?: boolean }) {
  const [internalOpen, setInternalOpen] = useState(false)
  const open = controlledOpen ?? internalOpen
  const setOpen: (value: boolean) => void = onOpenChange ?? setInternalOpen
  const [step, setStep] = useState(0)
  const [saving, setSaving] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState("")
  const [mailTypeDraft, setMailTypeDraft] = useState("")
  // Dieselben Felder wie im Bearbeitungs-Dialog: Einrichtung und Bearbeitung
  // dürfen nicht unterschiedlich reduziert sein.
  const [form, setForm] = useState({ name: "STRATO", host: "imap.strato.de", port: "993", username: "", password: "", purpose: "", industry: "", languages: ["Deutsch"], mailTypes: [] as string[], whitelist: "", context: "", unexpected: "" })
  const steps = ["Verbindung", "Profil", "Prüfen"]
  const trustedSenders = form.whitelist.split(/[\n,;]/).map((value) => value.trim()).filter(Boolean)
  const addMailTypeValue = (value: string) => {
    const trimmed = value.trim()
    if (!trimmed || form.mailTypes.includes(trimmed)) return
    setForm({ ...form, mailTypes: [...form.mailTypes, trimmed] })
  }
  const addMailType = () => {
    addMailTypeValue(mailTypeDraft)
    setMailTypeDraft("")
  }
  const removeMailType = (value: string) => setForm({ ...form, mailTypes: form.mailTypes.filter((item) => item !== value) })
  const save = async () => {
    setSaving(true)
    setError("")
    const id = crypto.randomUUID()
    try {
      await agentRequest("POST", "/v1/accounts", { account: { id, name: form.name, host: form.host, port: Number(form.port) || 993, username: form.username, inboxFolder: "INBOX", sentFolder: "Sent", spamFolder: "AI_SPAM_FILTER", safetyMode: "safe", enabled: true, dryRun: true, aiEnabled: true, ollamaValidated: false, profile: { purpose: form.purpose, context: form.context, unexpected: form.unexpected, industry: form.industry, languages: form.languages, expectedMailTypes: form.mailTypes, trustedDomains: [], trustedSenders, deniedSenders: [], deniedDomains: [], deniedKeywords: [], wantedNewsletters: [], legitimateAutomated: [] } }, password: form.password })
      setOpen(false)
      setStep(0)
      await refresh()
      onCreated?.(id)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setSaving(false)
    }
  }
  return <Dialog open={open} onOpenChange={(nextOpen) => { setOpen(nextOpen); if (!nextOpen) setStep(0) }}>
    {!hideTrigger && <DialogTrigger render={<Button size="icon-sm" aria-label="Postfach hinzufügen"><Plus /></Button>} />}
    <DialogContent className="flex max-h-[88vh] flex-col overflow-hidden border-white/[0.08] bg-[#1d1d1d] p-6 sm:max-w-[640px]">
      <DialogHeader><DialogTitle>Postfach verbinden</DialogTitle><DialogDescription>Schritt {step + 1} von {steps.length} · {steps[step]}</DialogDescription></DialogHeader>
      <div className="grid grid-cols-3 gap-2 py-2">{steps.map((label, index) => <div key={label}><div className={`h-1 rounded-full ${index <= step ? "bg-white" : "bg-white/10"}`} /><p className={`mt-2 text-[11px] ${index === step ? "text-white" : "text-[#666]"}`}>{label}</p></div>)}</div>
      <div className="min-h-0 flex-1 overflow-y-auto py-3 [scrollbar-gutter:stable]">
      {step === 0 && <div className="space-y-5">
        <Field label="Name des Postfachs"><Input className="h-12 px-3.5" placeholder="Zum Beispiel STRATO Geschäftlich" value={form.name} onChange={(e) => setForm({...form,name:e.target.value})} /></Field>
        <div className="grid grid-cols-[1fr_160px] gap-4"><Field label="IMAP-Server"><Input className="h-12 px-3.5" value={form.host} onChange={(e) => setForm({...form,host:e.target.value})} /></Field><Field label="Port"><Input className="h-12 px-3.5" inputMode="numeric" value={form.port} onChange={(e) => setForm({...form,port:e.target.value})} /></Field></div>
        <Field label="E-Mail / Benutzername"><Input className="h-12 px-3.5" placeholder="name@beispiel.de" value={form.username} onChange={(e) => setForm({...form,username:e.target.value})} /></Field>
        <div className="space-y-2">
          <div className="flex items-center gap-2"><Label>App-Passwort</Label><InfoTooltip><p>Die Zugangsdaten werden im Schlüsselbund des Betriebssystems gespeichert. Die Ersteinrichtung beginnt im Trockenlauf.</p></InfoTooltip></div>
          <div className="relative"><Input className="h-12 px-3.5 pr-11" autoComplete="new-password" type={showPassword ? "text" : "password"} value={form.password} onChange={(e) => setForm({...form,password:e.target.value})} /><button type="button" className="absolute right-3 top-1/2 -translate-y-1/2 text-[#777] transition-colors hover:text-white" onClick={() => setShowPassword((visible) => !visible)} aria-label={showPassword ? "Passwort ausblenden" : "Passwort anzeigen"}>{showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}</button></div>
        </div>
      </div>}
      {step === 1 && <div className="space-y-6">
        <Field label="Zweck des Postfachs"><Textarea className="min-h-20 px-3.5 py-3" placeholder="Zum Beispiel: Kundenanfragen, Angebote und Rechnungen einer Design-Agentur für Websites und Logos" value={form.purpose} onChange={(e) => setForm({...form,purpose:e.target.value})} /></Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Branche"><Input className="h-12 px-3.5" placeholder="Zum Beispiel Design und Medien" value={form.industry} onChange={(e) => setForm({...form,industry:e.target.value})} /></Field>
          <Field label="Erwartete Sprachen"><LanguageMultiSelect value={form.languages} onChange={(next) => setForm({ ...form, languages: next })} /></Field>
        </div>
        <Field label="Welche Mails erwartest du?">
          <div className="flex gap-2">
            <Input className="h-12 flex-1 px-3.5" placeholder="Mailtyp eingeben, Enter drücken" value={mailTypeDraft} onChange={(event) => setMailTypeDraft(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") { event.preventDefault(); addMailType() } }} />
            <button type="button" aria-label="Mailtyp hinzufügen" onClick={addMailType} disabled={!mailTypeDraft.trim()} className="flex h-12 w-12 shrink-0 items-center justify-center rounded-md border border-white/10 bg-white/[0.05] text-[#a8a8a8] transition-colors hover:bg-white/[0.08] hover:text-white disabled:pointer-events-none disabled:opacity-40"><CornerDownLeft className="size-4" /></button>
          </div>
          {form.mailTypes.length > 0 && <div className="mt-2 flex gap-1.5 overflow-x-auto pb-1.5">{form.mailTypes.map((type) => <span key={type} className="flex shrink-0 items-center gap-1.5 rounded-md border border-white/10 bg-white/[0.04] px-2.5 py-1 text-xs text-[#ccc]">{type}<button type="button" aria-label={`${type} entfernen`} onClick={() => removeMailType(type)} className="text-[#777] transition-colors hover:text-white"><X className="size-3" /></button></span>)}</div>}
          <div className="mt-0.5 flex flex-wrap items-center gap-1.5">{mailTypePresets.filter((preset) => !form.mailTypes.includes(preset)).map((preset) => <button key={preset} type="button" onClick={() => addMailTypeValue(preset)} className="flex items-center gap-1 rounded-md border border-dashed border-white/15 px-2.5 py-1 text-xs text-[#777] transition-colors hover:border-white/30 hover:text-[#ccc]"><Plus className="size-3" />{preset}</button>)}</div>
        </Field>
        <Field label="Vertrauenswürdige Absender (optional)"><Textarea className="min-h-16 px-3.5 py-3" placeholder={"Eine E-Mail-Adresse pro Zeile\nlieferant@beispiel.de"} value={form.whitelist} onChange={(e) => setForm({...form,whitelist:e.target.value})} /></Field>
        <Field label="Ungewöhnlich, aber legitim"><Textarea className="min-h-16 px-3.5 py-3" placeholder="Zum Beispiel: Newsletter von Design-Blogs, Rechnungen vom Hosting-Anbieter" value={form.context} onChange={(e) => setForm({...form,context:e.target.value})} /></Field>
        <div className="space-y-2">
          <div className="flex items-center gap-2"><Label>Was erwartest du hier niemals? (optional)</Label><InfoTooltip><p>Die KI leitet daraus ab, welche Werbekampagnen für dieses Postfach grundsätzlich fremd sind – je konkreter, desto besser.</p></InfoTooltip></div>
          <Textarea className="min-h-16 px-3.5 py-3" placeholder="Zum Beispiel: Diät-Werbung, Krypto-Anlagen, Krankenkassen-Lockangebote, Kaltakquise von Agenturen" value={form.unexpected} onChange={(e) => setForm({...form,unexpected:e.target.value})} />
        </div>
      </div>}
      {step === 2 && <div className="space-y-4">
        <div className="rounded-[10px] border border-white/10 bg-[#202020] p-4"><p className="text-sm font-medium">{form.name || "Postfach"}</p><p className="mt-1 text-xs text-[#666]">{form.username} · {form.host}:{form.port}</p></div>
        <div className="space-y-1.5 text-xs leading-5 text-[#888]">
          <p>Zweck: {form.purpose || "–"}</p>
          <p>Branche: {form.industry || "–"} · Sprachen: {form.languages.join(", ") || "–"}</p>
          <p>Mailtypen: {form.mailTypes.join(", ") || "–"}</p>
          <p>Whitelist: {trustedSenders.length} Einträge</p>
          <p>Niemals erwartet: {form.unexpected || "–"}</p>
        </div>
        {error && <p className="text-xs leading-5 text-[#e07a5f]">{error}</p>}
      </div>}
      </div>
      <DialogFooter className="border-t border-white/[0.09] pt-4"><Button variant="ghost" onClick={() => step === 0 ? setOpen(false) : setStep((current) => current - 1)}>{step === 0 ? "Abbrechen" : "Zurück"}</Button>{step < steps.length - 1 ? <Button disabled={step === 0 && (!form.host || !form.username || !form.password)} onClick={() => setStep((current) => current + 1)}>Weiter</Button> : <Button disabled={saving} onClick={() => void save()}>{saving ? "Speichere…" : "Verbinden"}</Button>}</DialogFooter>
    </DialogContent>
  </Dialog>
}

function Field({ label, children }: { label: string; children: React.ReactNode }) { return <div className="space-y-2"><Label>{label}</Label>{children}</div> }
