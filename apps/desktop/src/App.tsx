import { useEffect, useMemo, useRef, useState } from "react"
import { Archive, ArchiveRestore, ArrowDown, ArrowUp, ArrowUpDown, Bell, BellDot, Bot, CalendarDays, Check, ChevronDown, ChevronRight, ChevronsUpDown, CircleDot, Eye, EyeOff, Gauge, Globe2, Inbox, Info, LayoutDashboard, ListFilter, Mail, MailCheck, MailOpen, Monitor, PanelLeftClose, Pencil, Plus, RefreshCw, RotateCcw, Search, Settings, ShieldCheck, Tag, Table2, Text, Trash2, TriangleAlert, X } from "lucide-react"
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
import { agentRequest, demoDecisions, demoSummary, isTauri } from "@/lib/api"
import type { Account, Decision, SafetyMode, Summary } from "@/lib/api"

type Page = "dashboard" | "review" | "notifications" | "settings"
type Range = "week" | "month" | "year" | "all"
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

async function openDefaultMailClient() {
  if (isTauri()) {
    const { open } = await import("@tauri-apps/plugin-shell")
    await open("mailto:")
    return
  }
  window.location.href = "mailto:"
}

const chartDataByPeriod: Record<string, Array<{ label: string; spam: number; inbox: number; falsePositive: number }>> = {
  Tag: ["00", "04", "08", "12", "16", "20"].map((label, index) => ({ label, spam: [1, 0, 3, 5, 4, 2][index], inbox: [5, 3, 18, 27, 24, 14][index], falsePositive: [0, 0, 0, 1, 0, 0][index] })),
  Woche: ["Mo", "Di", "Mi", "Do", "Fr", "Sa", "So"].map((label, index) => ({ label, spam: [8, 17, 11, 23, 14, 6, 7][index], inbox: [54, 68, 61, 77, 70, 39, 31][index], falsePositive: [0, 1, 0, 1, 0, 0, 0][index] })),
  Monat: ["KW 1", "KW 2", "KW 3", "KW 4"].map((label, index) => ({ label, spam: [62, 81, 74, 93][index], inbox: [420, 486, 451, 528][index], falsePositive: [2, 3, 1, 4][index] })),
  Jahr: ["Jan", "Mär", "Mai", "Jul", "Sep", "Nov"].map((label, index) => ({ label, spam: [231, 284, 318, 296, 347, 371][index], inbox: [2030, 2180, 2340, 2210, 2470, 2590][index], falsePositive: [9, 11, 8, 13, 10, 12][index] })),
  Gesamt: ["2022", "2023", "2024", "2025", "2026"].map((label, index) => ({ label, spam: [1820, 2460, 3110, 3840, 2730][index], inbox: [16800, 20100, 24800, 29100, 22400][index], falsePositive: [78, 92, 108, 126, 81][index] })),
}

const notifications = [
  { id: "weekly-analysis", title: "Wochenanalyse abgeschlossen", detail: "438 Nachrichten geprüft, 86 als Spam zugeordnet.", time: "Heute, 16:00", action: false },
  { id: "review-required", title: "12 Fälle benötigen eine Prüfung", detail: "Die Bewertung war für eine automatische Zuordnung nicht sicher genug.", time: "Heute, 15:58", action: true },
  { id: "model-available", title: "Lokales Modell verfügbar", detail: "qwen3:4b-instruct antwortet und kann validiert werden.", time: "Gestern", action: false },
]

export default function App() {
  const mainScrollRef = useRef<HTMLDivElement>(null)
  const mainFade = useScrollFade(mainScrollRef)
  const [page, setPage] = useState<Page>("dashboard")
  const [summary, setSummary] = useState<Summary>(demoSummary)
  const [decisions, setDecisions] = useState<Decision[]>(demoDecisions)
  const [accounts, setAccounts] = useState<Account[]>([])
  const [, setAgentOnline] = useState(false)
  const [compactNav, setCompactNav] = useState(false)
  const narrowApp = useMediaQuery("(max-width: 890px)")
  const effectiveCompactNav = narrowApp || compactNav

  const refresh = async () => {
    if (!isTauri()) return
    try {
      const [nextSummary, nextDecisions, nextAccounts] = await Promise.all([
        agentRequest<Summary>("GET", "/v1/summary"),
        agentRequest<Decision[]>("GET", "/v1/decisions?limit=250"),
        agentRequest<Account[]>("GET", "/v1/accounts"),
      ])
      setSummary(nextSummary)
      setDecisions(nextDecisions ?? [])
      setAccounts(nextAccounts ?? [])
      setAgentOnline(true)
    } catch {
      setAgentOnline(false)
    }
  }

  useEffect(() => {
    const initial = window.setTimeout(() => void refresh(), 0)
    const retry = isTauri() ? window.setTimeout(() => void refresh(), 1200) : undefined
    return () => {
      window.clearTimeout(initial)
      if (retry !== undefined) window.clearTimeout(retry)
    }
  }, [])

  return (
    <TooltipProvider>
      <div className="flex h-screen min-h-[620px] overflow-hidden bg-[#171717] text-white">
        <Sidebar page={page} onPage={setPage} compact={effectiveCompactNav} compactLocked={narrowApp} onCompact={() => setCompactNav((value) => !value)} pending={summary.pending} />
        <main className="relative min-w-0 flex-1 overflow-hidden">
          <div ref={mainScrollRef} className="h-full overflow-y-auto">
          {page === "settings" && <Header page={page} />}
          <div className={`mx-auto w-full max-w-[1500px] px-14 max-[639px]:px-7 ${page === "review" ? "h-screen overflow-hidden pb-0 pt-12" : page === "notifications" ? "pb-10 pt-12" : page === "settings" ? "h-[calc(100vh-100px)] overflow-hidden pb-0 pt-12" : "pb-10 pt-12"}`}>
            {page === "dashboard" && <Dashboard summary={summary} onReview={() => setPage("review")} scrollRef={mainScrollRef} />}
            {page === "review" && <ReviewPage decisions={decisions} refresh={refresh} />}
            {page === "notifications" && <Notifications scrollRef={mainScrollRef} />}
            {page === "settings" && <SettingsPage accounts={accounts} refresh={refresh} />}
          </div>
          </div>
          {page !== "review" && page !== "settings" && <ScrollFade strength={mainFade} targetRef={mainScrollRef} />}
        </main>
      </div>
    </TooltipProvider>
  )
}

function Sidebar({ page, onPage, compact, compactLocked, onCompact, pending }: { page: Page; onPage: (page: Page) => void; compact: boolean; compactLocked: boolean; onCompact: () => void; pending: number }) {
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
          {secondary.map((item) => <NavItem key={item.id} {...item} active={page === item.id} compact={compact} onClick={() => onPage(item.id)} />)}
        </nav>
        <AccountSwitcher compact={compact} />
      </div>
    </aside>
  )
}

function NavItem({ id, label, icon: Icon, active, compact, onClick, badge }: { id: string; label: string; icon: typeof Bell; active: boolean; compact: boolean; onClick: () => void; badge?: number }) {
  const button = <button onClick={onClick} aria-label={compact ? label : undefined} className={`flex h-11 w-full items-center rounded-md border text-sm outline-none transition-colors focus-visible:border-white/20 ${compact ? "justify-center px-0" : "gap-3 px-3.5"} ${active ? "border-white/10 bg-white/[0.06] text-white" : "border-transparent text-[#a8a8a8] hover:border-white/10 hover:bg-white/[0.04] hover:text-white"}`}>
    <span className="relative shrink-0"><Icon className="size-4" />{id === "notifications" && <span className="absolute -right-0.5 -top-0.5 size-1.5 rounded-full bg-[#ff6b2c] ring-2 ring-[#1d1d1d]" />}</span>{!compact && <><span className="truncate">{label}</span>{badge ? <span className="ml-auto flex min-w-[30px] items-center justify-center rounded-[4px] bg-white px-2 py-0.5 text-[11px] font-medium text-[#666]">{badge}</span> : null}</>}
  </button>
  if (!compact) return button
  return <Tooltip><TooltipTrigger render={button} /><TooltipContent side="right" sideOffset={10}>{label}</TooltipContent></Tooltip>
}

function AccountSwitcher({ compact }: { compact: boolean }) {
  return <div className="mt-4">
    <DropdownMenu>
      <DropdownMenuTrigger render={<button aria-label="Postfach wechseln" className={`flex w-full items-center rounded-md border border-white/10 bg-white/[0.05] outline-none transition-colors hover:bg-white/[0.07] ${compact ? "h-11 justify-center p-1" : "h-[54px] gap-3 px-1.5 pr-3.5"}`} />}>
        <span className={`flex shrink-0 items-center justify-center rounded-md border border-white/10 bg-[#ff4d00] text-xs font-medium text-black ${compact ? "size-[34px]" : "size-10"}`}>ST</span>
        {!compact && <><span className="min-w-0 flex-1 truncate text-left text-sm text-[#a8a8a8]">kontakt@fliesenbetrieb.de</span><ChevronsUpDown className="size-4 text-[#777]" /></>}
      </DropdownMenuTrigger>
      <DropdownMenuContent side={compact ? "right" : "top"} align="start" className="min-w-[250px]"><DropdownMenuItem><span className="mr-2 flex size-7 items-center justify-center rounded bg-[#ff4d00] text-[10px] text-black">ST</span>STRATO Postfach</DropdownMenuItem><DropdownMenuItem><Plus />Postfach hinzufügen</DropdownMenuItem></DropdownMenuContent>
    </DropdownMenu>
  </div>
}

function Header({ page }: { page: Page }) {
  const titles: Record<Page, string> = { dashboard: "Übersicht", review: "Zuordnung", notifications: "Benachrichtigungen", settings: "Einstellungen" }
  return <header className="mx-auto flex h-[100px] w-full max-w-[1500px] items-start justify-between gap-6 px-14 pt-12 max-[639px]:px-7">
    <h1 className="pt-1 text-2xl font-medium tracking-tight">{titles[page]}</h1>
    {page === "settings" && <div className="relative h-[52px] w-[280px] shrink-0"><Input aria-label="Einstellungen durchsuchen" placeholder="Durchsuchen" className="h-full rounded-md border-white/10 bg-white/[0.05] px-3.5 pr-11 text-sm placeholder:text-[#888]" /><Search className="pointer-events-none absolute right-3.5 top-1/2 size-4 -translate-y-1/2 text-[#888]" /></div>}
  </header>
}

function Dashboard({ summary, onReview, scrollRef }: { summary: Summary; onReview: () => void; scrollRef: React.RefObject<HTMLElement | null> }) {
  const [period, setPeriod] = useState("Woche")
  const [showInbox, setShowInbox] = useState(true)
  const [showFalsePositives, setShowFalsePositives] = useState(true)
  const activeChartData = chartDataByPeriod[period]
  const totals = activeChartData.reduce((sum, item) => ({ spam: sum.spam + item.spam, inbox: sum.inbox + item.inbox, falsePositive: sum.falsePositive + item.falsePositive }), { spam: 0, inbox: 0, falsePositive: 0 })
  const spamShare = Math.round((totals.spam / (totals.spam + totals.inbox)) * 100)
  return <div className="dashboard-cards space-y-6">
    <div data-section-id="dashboard-summary" className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <Metric label="Spam zugeordnet" value={summary.moved + summary.confirmed} note="Diese Woche" />
      <Metric label="Noch zu prüfen" value={summary.pending} note="Menschliche Entscheidung" onClick={onReview} />
      <Metric label="Verarbeitet" value={summary.processedWeek} note="Letzte 7 Tage" />
      <Metric label="Fehlalarmrate" value={`${(summary.falsePositiveRate * 100).toFixed(1)} %`} note="Bestätigte Prüfungen" />
    </div>
    <div data-section-id="dashboard-analysis" className="grid gap-6 xl:grid-cols-[1.45fr_1fr]">
      <Card className="relative min-w-0 border-white/[0.07] bg-[#1b1b1b] shadow-none"><CardHeader className="pr-[340px]"><div><CardTitle>Spam</CardTitle><CardDescription>Spam im Verhältnis zum normalen Eingang</CardDescription></div><div className="absolute right-6 top-6"><Segmented options={["Tag", "Woche", "Monat", "Jahr", "Gesamt"]} value={period} onChange={setPeriod} /></div></CardHeader><CardContent className="min-w-0 pt-2"><div className="mb-4 flex flex-wrap items-end justify-between gap-4"><div className="flex items-baseline gap-3"><span className="text-3xl font-medium text-[#ff6b2c]">{spamShare} %</span><span className="text-xs text-[#777]">Spam · {totals.spam.toLocaleString("de-DE")} Nachrichten</span></div><div className="flex flex-wrap gap-2 text-xs"><span className="flex items-center gap-2 rounded-md px-2.5 py-1.5 text-[#ff6b2c]"><span className="size-2 rounded-full bg-[#ff6b2c]" />Spam</span><button onClick={() => setShowInbox((value) => !value)} className={`flex items-center gap-2 rounded-md px-2.5 py-1.5 transition-colors ${showInbox ? "bg-white/[0.05] text-[#d6d6d6]" : "text-[#555]"}`}><span className={`size-2 rounded-full ${showInbox ? "bg-[#d6d6d6]" : "bg-[#555]"}`} />Eingang · {totals.inbox.toLocaleString("de-DE")}</button><button onClick={() => setShowFalsePositives((value) => !value)} className={`flex items-center gap-2 rounded-md px-2.5 py-1.5 transition-colors ${showFalsePositives ? "bg-white/[0.05] text-[#858585]" : "text-[#4d4d4d]"}`}><span className={`size-2 rounded-full ${showFalsePositives ? "bg-[#858585]" : "bg-[#4d4d4d]"}`} />Fehlalarme · {totals.falsePositive.toLocaleString("de-DE")}</button></div></div><div className="h-[240px]"><ResponsiveContainer width="100%" height="100%" minWidth={0} minHeight={0} initialDimension={{ width: 640, height: 240 }}><AreaChart data={activeChartData}><defs><linearGradient id="spamArea" x1="0" y1="0" x2="0" y2="1"><stop offset="5%" stopColor="#ff6b2c" stopOpacity={0.42} /><stop offset="95%" stopColor="#ff6b2c" stopOpacity={0.02} /></linearGradient><linearGradient id="inboxArea" x1="0" y1="0" x2="0" y2="1"><stop offset="5%" stopColor="#d6d6d6" stopOpacity={0.16} /><stop offset="95%" stopColor="#d6d6d6" stopOpacity={0.01} /></linearGradient></defs><CartesianGrid vertical={false} stroke="rgba(255,255,255,.055)" /><XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ fill: "#777", fontSize: 12 }} /><YAxis axisLine={false} tickLine={false} tick={{ fill: "#666", fontSize: 11 }} width={34} /><ChartTooltip cursor={false} contentStyle={{ background: "#242424", border: "1px solid rgba(255,255,255,.1)", borderRadius: 8, fontSize: 12 }} />{showInbox && <Area type="natural" dataKey="inbox" name="Eingang" fill="url(#inboxArea)" stroke="#d6d6d6" strokeWidth={1.5} dot={false} />}{showFalsePositives && <Area type="natural" dataKey="falsePositive" name="Fehlalarme" fill="transparent" stroke="#858585" strokeWidth={1.5} strokeDasharray="4 4" dot={false} />}<Area type="natural" dataKey="spam" name="Spam" fill="url(#spamArea)" stroke="#ff6b2c" strokeWidth={2} dot={false} activeDot={{ r: 4, fill: "#ff6b2c" }} /></AreaChart></ResponsiveContainer></div></CardContent></Card>
      <Card className="border-white/[0.07] bg-[#1b1b1b] shadow-none"><CardHeader><CardTitle>Letzte Benachrichtigungen</CardTitle><CardDescription>Lokale Ereignisse und offene Aufgaben</CardDescription></CardHeader><CardContent className="divide-y divide-white/[0.06]">{notifications.map((item) => <div key={item.title} className="flex gap-3 py-4 first:pt-0"><div className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg bg-white/[0.05]">{item.action ? <BellDot className="size-4" /> : <Check className="size-4 text-[#999]" />}</div><div className="min-w-0 flex-1"><p className="text-sm font-medium">{item.title}</p><p className="mt-1 text-xs leading-5 text-[#888]">{item.detail}</p><p className="mt-2 text-[11px] text-[#555]">{item.time}</p></div></div>)}</CardContent></Card>
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

function ReviewPage({ decisions, refresh }: { decisions: Decision[]; refresh: () => void }) {
  const tableScrollRef = useRef<HTMLDivElement>(null)
  const tableFade = useScrollFade(tableScrollRef)
  const tableHorizontalFade = useScrollFade(tableScrollRef, "horizontal")
  const toolbarScrollRef = useRef<HTMLDivElement>(null)
  const toolbarFade = useScrollFade(toolbarScrollRef, "horizontal")
  const [view, setView] = useState<"review" | "spam">("review")
  const [reviewFilter, setReviewFilter] = useState<"review" | "rejected">("review")
  const [range, setRange] = useState<Range>("week")
  const [filterOpen, setFilterOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [selected, setSelected] = useState<string[]>([])
  const [referenceTime] = useState(() => Date.now())
  const [sort, setSort] = useState<{ key: SortKey; direction: SortDirection }>({ key: "receivedAt", direction: "desc" })
  const filtersActive = range !== "week" || (view === "review" && reviewFilter !== "review")
  const resetFilters = () => { setRange("week"); setReviewFilter("review"); setSelected([]) }
  const filtered = useMemo(() => {
    const days = range === "week" ? 7 : range === "month" ? 31 : range === "year" ? 366 : Infinity
    return decisions.filter((item) => item.score >= 0.6 && (view === "review" ? (reviewFilter === "review" ? ["pending", "moved"].includes(item.status) : item.status === "rejected") : item.status === "confirmed") && referenceTime - new Date(item.receivedAt).getTime() <= days * 86_400_000 && `${item.from} ${item.subject}`.toLowerCase().includes(query.toLowerCase())).sort((a, b) => {
      if (!sort.direction) return 0; const left = sort.key === "category" ? spamCategory(a) : a[sort.key]; const right = sort.key === "category" ? spamCategory(b) : b[sort.key]; const result = typeof left === "number" ? left - Number(right) : String(left).localeCompare(String(right), "de"); return sort.direction === "asc" ? result : -result
    })
  }, [decisions, view, reviewFilter, range, query, sort, referenceTime])
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
        <ToolbarButton iconOnly onClick={() => void refresh()} aria-label="Daten aktualisieren"><RefreshCw className="size-4" /></ToolbarButton>
        <div className="relative h-[52px] w-[300px] min-w-[220px] shrink-0">
          <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Durchsuchen" className="h-full rounded-md border-white/10 bg-white/[0.05] px-3.5 pr-11 text-sm placeholder:text-[#888]" />
          <Search className="pointer-events-none absolute right-3.5 top-1/2 size-4 -translate-y-1/2 text-[#888]" />
        </div>
        <div className="flex shrink-0 items-center">
          <FilterToolbarButton active={filtersActive} open={filterOpen} onToggle={() => setFilterOpen((current) => !current)} onReset={resetFilters} />
          {filterOpen && view === "review" && <><ToolbarConnector /><Select value={reviewFilter} onValueChange={(value) => { setSelected([]); setReviewFilter(value as "review" | "rejected") }}><SelectTrigger className={`h-[52px]! min-w-[148px] shrink-0 rounded-md px-3.5 text-sm! ${reviewFilter !== "review" ? "border-white! bg-white! text-[#171717]! hover:bg-white/90! [&_svg]:text-[#171717]!" : "border-white/10 bg-white/[0.05] text-[#aaa]"}`}><SelectValue>{reviewFilter === "review" ? "Review" : "Kein Spam"}</SelectValue></SelectTrigger><SelectContent><SelectItem value="review">Review</SelectItem><SelectItem value="rejected">Kein Spam</SelectItem></SelectContent></Select></>}
          {filterOpen && <><ToolbarConnector /><Select value={range} onValueChange={(value) => setRange(value as Range)}><SelectTrigger className={`h-[52px]! min-w-[148px] shrink-0 rounded-md px-3.5 text-sm! ${range !== "week" ? "border-white! bg-white! text-[#171717]! hover:bg-white/90! [&_svg]:text-[#171717]!" : "border-white/10 bg-white/[0.05] text-[#aaa]"}`}><SelectValue>{({ week: "Woche", month: "Monat", year: "Jahr", all: "Alles" } as const)[range]}</SelectValue></SelectTrigger><SelectContent><SelectItem value="week">Woche</SelectItem><SelectItem value="month">Monat</SelectItem><SelectItem value="year">Jahr</SelectItem><SelectItem value="all">Alles</SelectItem></SelectContent></Select></>}
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
            const active = selected.includes(item.id)
            return <TableRow key={item.id} data-state={active ? "selected" : undefined} className="h-16 border-white/[0.09] bg-transparent text-[#a8a8a8] hover:bg-white/[0.025] data-[state=selected]:bg-[#101010] data-[state=selected]:text-white">
              <TableCell className={`px-4 ${shortDivider}`}><Checkbox checked={active} onCheckedChange={() => setSelected((current) => current.includes(item.id) ? current.filter((id) => id !== item.id) : [...current, item.id])} /></TableCell>
              <TableCell className={`px-3 text-sm ${shortDivider}`}><div className="flex min-w-0 items-center gap-2"><Tooltip><TooltipTrigger render={<button className="flex size-7 shrink-0 items-center justify-center rounded-md text-[#666] transition-colors hover:bg-white/[0.05] hover:text-white" aria-label="Mail öffnen" onClick={() => void openDefaultMailClient()}><MailOpen className="size-3.5" /></button>} /><TooltipContent side="top">Mail öffnen</TooltipContent></Tooltip><Tooltip><TooltipTrigger render={<span className="min-w-0 truncate" tabIndex={0}>{item.from}</span>} /><TooltipContent side="top">{item.from}</TooltipContent></Tooltip></div></TableCell>
              <TableCell className={`px-4 font-mono text-xs transition-colors ${shortDivider}`} style={{ color: scoreColor(item.score) }}>{Math.round(item.score * 100)} %</TableCell>
              <TableCell className={`truncate px-4 text-xs text-[#888] ${shortDivider}`}>{spamCategory(item)}</TableCell>
              <TableCell className={`px-4 ${shortDivider}`}><p className="truncate text-sm">{item.subject}</p><p className="mt-1 truncate text-xs text-[#666]">{item.evidence.map((entry) => entry.summary).join(" · ")}</p></TableCell>
              <TableCell className={`whitespace-nowrap px-4 text-xs text-[#888] ${shortDivider}`}>{new Intl.DateTimeFormat("de-DE", { day: "2-digit", month: "2-digit", year: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(item.receivedAt))}</TableCell>
              <TableCell className="px-4"><StatusBadge status={item.status} /></TableCell>
            </TableRow>
          })}{filtered.length === 0 && <TableRow><TableCell colSpan={7} className="h-40 text-center text-sm text-[#666]">Keine Nachrichten für diese Ansicht.</TableCell></TableRow>}</TableBody>
            </Table>
          </div>
        </div>
        <ScrollFade strength={tableFade} targetRef={tableScrollRef} />
        <ScrollFade strength={tableHorizontalFade} direction="horizontal" targetRef={tableScrollRef} />
      </div>
    </div>
    <FloatingActions visible={selected.length > 0} primary={view === "review" ? "Spam markieren" : "Kein Spam"} onPrimary={() => void review(view === "review" ? "confirm" : "reject")} onCancel={() => setSelected([])} />
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

function FloatingActions({ visible, primary, onPrimary, onCancel, disabled = false }: { visible: boolean; primary: string; onPrimary: () => void; onCancel: () => void; disabled?: boolean }) {
  const [mounted, setMounted] = useState(visible)
  useEffect(() => {
    if (visible) { setMounted(true); return }
    if (!mounted) return
    const timeout = window.setTimeout(() => setMounted(false), 500)
    return () => window.clearTimeout(timeout)
  }, [visible, mounted])
  if (!mounted) return null
  return <div className={`${visible ? "floating-action-enter" : "floating-action-exit"} absolute bottom-8 left-1/2 z-30 flex h-[52px] items-center rounded-md border border-white/10 bg-[#232323] p-1.5 shadow-[0_30px_60px_rgba(0,0,0,.45)]`}><button disabled={disabled} className="flex h-full items-center gap-2 rounded-l bg-white px-4 text-sm text-[#171717] disabled:cursor-not-allowed disabled:opacity-40" onClick={onPrimary}>{primary}<Check className="size-4" /></button><button className="flex h-full items-center gap-2 rounded-r bg-[#171717] px-4 text-sm text-[#a8a8a8]" onClick={onCancel}>Abbrechen<X className="size-4" /></button></div>
}

function ViewSwitch({ value, onChange }: { value: "review" | "spam"; onChange: (value: "review" | "spam") => void }) {
  return <div className="flex h-[52px] items-center rounded-md border border-white/10 bg-[#232323] p-1.5"><button onClick={() => onChange("review")} className={`flex h-full items-center gap-2 rounded-l px-3.5 text-sm ${value === "review" ? "bg-white text-[#171717]" : "bg-[#171717] text-[#a8a8a8]"}`}>Review<Eye className="size-4" /></button><button onClick={() => onChange("spam")} className={`flex h-full items-center gap-2 rounded-r px-3.5 text-sm ${value === "spam" ? "bg-white text-[#171717]" : "bg-[#171717] text-[#a8a8a8]"}`}>Spam<Trash2 className="size-4" /></button></div>
}

function ToolbarButton({ children, iconOnly = false, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & { iconOnly?: boolean }) {
  return <button {...props} className={`flex h-[52px] shrink-0 items-center justify-center gap-2 rounded-md border border-white/10 bg-white/[0.05] text-[14px] text-[#a8a8a8] outline-none transition-colors hover:bg-white/[0.08] hover:text-white focus-visible:border-white/20 ${iconOnly ? "w-[52px]" : "px-3.5"}`}>{children}</button>
}

function ToolbarConnector() { return <span aria-hidden className="h-px w-2 shrink-0 bg-white/10" /> }

function FilterToolbarButton({ active, open, onToggle, onReset }: { active: boolean; open: boolean; onToggle: () => void; onReset: () => void }) {
  return <div className={`flex h-[52px] shrink-0 items-center rounded-md border transition-colors ${active ? "border-white bg-white text-[#171717]" : "border-white/10 bg-white/[0.05] text-[#a8a8a8]"}`}>
    <button className="h-full px-3.5 text-[14px]" onClick={onToggle} aria-expanded={open}>Filter</button>
    <button className={`mr-1 flex size-9 items-center justify-center rounded-md transition-colors ${active ? "hover:bg-black/10" : "hover:bg-white/[0.07] hover:text-white"}`} onClick={active ? onReset : onToggle} aria-label={active ? "Filter zurücksetzen" : "Filter öffnen"}>{active ? <RotateCcw className="size-4" /> : <ListFilter className="size-4" />}</button>
  </div>
}

function SortableHead({ label, name, sort, setSort, icon: HeadIcon, last = false }: { label: string; name: SortKey; sort: { key: SortKey; direction: SortDirection }; setSort: (value: { key: SortKey; direction: SortDirection }) => void; icon: typeof Mail; last?: boolean }) {
  const Icon = sort.key !== name || !sort.direction ? ArrowUpDown : sort.direction === "asc" ? ArrowUp : ArrowDown
  const update = (direction: SortDirection) => setSort({ key: name, direction })
  return <div role="columnheader" className={`flex h-full items-center px-4 ${last ? "" : shortDivider}`}><DropdownMenu><DropdownMenuTrigger render={<button className="flex w-full items-center justify-between gap-2 text-xs text-[#a8a8a8] hover:text-white" />}><span className="flex min-w-0 items-center gap-2"><HeadIcon className="size-3.5 shrink-0" /><span className="truncate">{label}</span></span><Icon className="size-3.5 shrink-0" /></DropdownMenuTrigger><DropdownMenuContent align="start"><DropdownMenuItem onClick={() => update("asc")}><ArrowUp />Aufsteigend</DropdownMenuItem><DropdownMenuItem onClick={() => update("desc")}><ArrowDown />Absteigend</DropdownMenuItem><DropdownMenuItem onClick={() => update(null)}><RefreshCw />Zurücksetzen</DropdownMenuItem></DropdownMenuContent></DropdownMenu></div>
}

function StatusBadge({ status }: { status: Decision["status"] }) { const labels = { pending: "Review", moved: "Review", confirmed: "Bestätigt", rejected: "Fehlalarm", deferred: "Später" }; return <Badge variant="outline" className="border-white/10 bg-white/[0.025] text-[#aaa]">{labels[status]}</Badge> }

function Notifications({ scrollRef }: { scrollRef: React.RefObject<HTMLElement | null> }) {
  const [view, setView] = useState<"open" | "archived">("open")
  const [archived, setArchived] = useState<Record<string, number>>(() => {
    try {
      const stored = JSON.parse(readStoredValue("mailmune.notificationArchive", "spamalytic.notificationArchive", "{}")) as Record<string, number>
      const cutoff = Date.now() - 180 * 86_400_000
      return Object.fromEntries(Object.entries(stored).filter(([, archivedAt]) => archivedAt >= cutoff).slice(-500))
    } catch { return {} }
  })
  useEffect(() => { localStorage.setItem("mailmune.notificationArchive", JSON.stringify(archived)) }, [archived])
  const visible = notifications.filter((item) => view === "archived" ? Boolean(archived[item.id]) : !archived[item.id])
  const notificationSections = visible.map((item) => ({ id: `notification-${item.id}`, label: item.title }))
  return <div className="relative">
    <div className="flex items-start justify-between gap-6"><h1 className="pt-1 text-2xl font-medium tracking-tight">Benachrichtigungen</h1><div className="flex h-[52px] items-center rounded-md border border-white/10 bg-[#232323] p-1.5"><button onClick={() => setView("open")} className={`flex h-full items-center gap-2 rounded-l px-3.5 text-sm ${view === "open" ? "bg-white text-[#171717]" : "bg-[#171717] text-[#a8a8a8]"}`}>Offen<Bell className="size-4" /></button><button onClick={() => setView("archived")} className={`flex h-full items-center gap-2 rounded-r px-3.5 text-sm ${view === "archived" ? "bg-white text-[#171717]" : "bg-[#171717] text-[#a8a8a8]"}`}>Archiviert<Archive className="size-4" /></button></div></div>
    <div className="mt-12 max-w-4xl">
    <div className="border-y border-white/[0.09]">{visible.map((item) => <div key={item.id} data-section-id={`notification-${item.id}`} className="flex items-center gap-4 border-b border-white/[0.09] py-5 last:border-b-0"><div className="relative flex size-9 shrink-0 items-center justify-center text-[#999]"><Bell className="size-4" />{item.action && <span className="absolute right-1 top-1 size-1.5 rounded-full bg-[#ff6b2c]" />}</div><div className="min-w-0 flex-1"><p className="text-sm font-medium">{item.title}</p><p className="mt-1 text-xs leading-5 text-[#777]">{item.detail}</p><p className="mt-2 text-[11px] text-[#555]">{item.time}</p></div>{item.action && view === "open" && <Button size="sm" variant="outline">Prüfen</Button>}<button className="flex size-9 shrink-0 items-center justify-center rounded-md text-[#777] transition-colors hover:bg-white/[0.05] hover:text-white" aria-label={view === "open" ? "Benachrichtigung archivieren" : "Benachrichtigung wiederherstellen"} onClick={() => setArchived((current) => { const next = { ...current }; if (view === "open") next[item.id] = Date.now(); else delete next[item.id]; return next })}>{view === "open" ? <X className="size-4" /> : <ArchiveRestore className="size-4" />}</button></div>)}{visible.length === 0 && <p className="py-12 text-center text-sm text-[#666]">{view === "open" ? "Keine offenen Benachrichtigungen." : "Keine archivierten Benachrichtigungen."}</p>}</div>
    {view === "archived" && <p className="mt-3 text-right text-[11px] text-[#555]">Archivierte Einträge werden nach 180 Tagen entfernt.</p>}
    </div>
    <SectionIndicator items={notificationSections} scrollRef={scrollRef} />
  </div>
}

function SettingsPage({ accounts, refresh }: { accounts: Account[]; refresh: () => void }) {
  const settingsScrollRef = useRef<HTMLDivElement>(null)
  const settingsFade = useScrollFade(settingsScrollRef)
  const { theme, setTheme } = useTheme()
  const [mode, setMode] = useState<SafetyMode>(accounts[0]?.safetyMode ?? "safe")
  const [folderName, setFolderName] = useState(accounts[0]?.spamFolder ?? "AI_SPAM_FILTER")
  const [folderDraft, setFolderDraft] = useState(folderName)
  const [editingFolder, setEditingFolder] = useState(false)
  const [notificationThreshold, setNotificationThreshold] = useState(() => Number(readStoredValue("mailmune.notificationThreshold", "spamalytic.notificationThreshold", "90")))
  const [automaticThreshold, setAutomaticThreshold] = useState(() => Number(readStoredValue("mailmune.automaticSpamThreshold.v2", "spamalytic.automaticSpamThreshold.v2", "90")))
  const [accountStatus, setAccountStatus] = useState<Record<string, string>>({})
  const [hiddenAccounts, setHiddenAccounts] = useState<string[]>([])
  const [fakeAccountVisible, setFakeAccountVisible] = useState(true)
  const [fakeModelVisible, setFakeModelVisible] = useState(true)
  const [connectionEnabled, setConnectionEnabled] = useState<Record<string, boolean>>({ "demo-strato": true, "demo-qwen": true })
  const [weeklyReviewEnabled, setWeeklyReviewEnabled] = useState(true)
  const [incomingReviewEnabled, setIncomingReviewEnabled] = useState(true)
  const [deleteTarget, setDeleteTarget] = useState<{ kind: "account" | "model"; id: string; label: string } | null>(null)
  const runAccountAction = async (account: Account, action: "test" | "scan") => {
    setAccountStatus((current) => ({ ...current, [account.id]: action === "test" ? "Verbindung wird geprüft …" : "Trockenlauf wird ausgeführt …" }))
    try {
      if (action === "test") {
        const result = await agentRequest<{ supportsIdle: boolean; supportsMove: boolean; folders: string[] }>("POST", `/v1/accounts/${account.id}/test`)
        setAccountStatus((current) => ({ ...current, [account.id]: `Verbunden · IDLE ${result.supportsIdle ? "verfügbar" : "nicht verfügbar"} · MOVE ${result.supportsMove ? "verfügbar" : "nicht verfügbar"}` }))
      } else {
        const result = await agentRequest<{ processed: number; candidates: number; moved: number; dryRun: boolean }>("POST", `/v1/accounts/${account.id}/scan`)
        setAccountStatus((current) => ({ ...current, [account.id]: `${result.processed} Nachrichten gelesen · ${result.candidates} Prüffälle · nichts verschoben` }))
        await refresh()
      }
    } catch (error) {
      setAccountStatus((current) => ({ ...current, [account.id]: error instanceof Error ? error.message : "Aktion fehlgeschlagen" }))
    }
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
      <SafetySlider mode={mode} setMode={(nextMode) => { setMode(nextMode); if (nextMode === "safe" && automaticThreshold < 90) { setAutomaticThreshold(90); localStorage.setItem("mailmune.automaticSpamThreshold.v2", "90") } }} />
      {mode !== "confirm_all" && <div className="mt-6"><AutomaticSpamThreshold value={automaticThreshold} onChange={(value) => { setAutomaticThreshold(value); localStorage.setItem("mailmune.automaticSpamThreshold.v2", String(value)); if (value < 90) setMode("aggressive") }} /></div>}
      <div className="mt-6"><NotificationStrength value={notificationThreshold} onChange={(value) => { setNotificationThreshold(value); localStorage.setItem("mailmune.notificationThreshold", String(value)) }} /></div>
      <div className="my-6 border-t border-white/[0.09]" />
      <div data-section-id="settings-folder" className="space-y-3">
        <div className="flex items-center gap-2"><h2 className="text-sm font-medium">Ordnername</h2><InfoTooltip><p>Ändert den IMAP-Zielordner und aktualisiert alle zugehörigen Verknüpfungen. Vorhandene Nachrichten werden dabei nicht gelöscht.</p></InfoTooltip></div>
        {editingFolder ? <input autoFocus aria-label="Ordnername" className="h-12 w-full rounded-[10px] border border-white/20 bg-[#242424] px-3.5 text-sm text-white outline-none focus:border-white/35" value={folderDraft} onChange={(event) => setFolderDraft(event.target.value)} /> : <button onClick={() => { setFolderDraft(folderName); setEditingFolder(true) }} className="flex h-12 w-full items-center justify-between rounded-[10px] border border-white/10 bg-[#242424] px-3.5 text-sm text-white/40 transition-colors hover:border-white/20 hover:text-white/70"><span>{folderName}</span><Pencil className="size-4" /></button>}
      </div>
      <div className="my-6 border-t border-white/[0.09]" />
      <div data-section-id="settings-scan" className="border-b border-white/[0.09] pb-6"><h2 className="mb-3 text-sm font-medium">Automatische Prüfung</h2><div className="space-y-3"><ConnectionCard icon={CalendarDays} title="Wochenprüfung" detail="Freitags, 16:00 Uhr" enabled={weeklyReviewEnabled} onEnabled={setWeeklyReviewEnabled} onSettings={() => {}} /><ConnectionCard icon={MailCheck} title="Bei Posteingang" detail="Neue Nachrichten direkt prüfen" enabled={incomingReviewEnabled} onEnabled={setIncomingReviewEnabled} onSettings={() => {}} /></div></div>
    </section>
    <section className="min-w-0 space-y-6">
      <div data-section-id="settings-account" className="border-b border-white/[0.09] pb-6"><ConnectionSection title="Postfach" count={accounts.filter((account) => !hiddenAccounts.includes(account.id)).length + (fakeAccountVisible && accounts.length === 0 ? 1 : 0)} add={<div className="flex items-center gap-1.5"><TransferPlaceholder kind="learning" /><TransferPlaceholder kind="profile" /><AddAccount refresh={refresh} /></div>}>
        {accounts.filter((account) => !hiddenAccounts.includes(account.id)).map((account) => <ConnectionCard key={account.id} icon={Inbox} title={account.name} detail={account.username} enabled={connectionEnabled[account.id] ?? account.enabled} onEnabled={(enabled) => setConnectionEnabled((current) => ({ ...current, [account.id]: enabled }))} onSettings={() => {}} onDelete={() => setDeleteTarget({ kind: "account", id: account.id, label: account.name })}><div className="mt-3 flex flex-wrap gap-2"><Button size="sm" variant="outline" onClick={() => void runAccountAction(account, "test")}>Verbindung testen</Button><Button size="sm" onClick={() => void runAccountAction(account, "scan")}>Jetzt prüfen</Button></div>{accountStatus[account.id] && <p className="mt-3 text-xs leading-5 text-[#888]">{accountStatus[account.id]}</p>}</ConnectionCard>)}
        {accounts.length === 0 && fakeAccountVisible && <ConnectionCard icon={Inbox} title="STRATO Postfach" detail="kontakt@fliesenbetrieb.de" enabled={connectionEnabled["demo-strato"]} onEnabled={(enabled) => setConnectionEnabled((current) => ({ ...current, "demo-strato": enabled }))} onSettings={() => {}} onDelete={() => setDeleteTarget({ kind: "account", id: "demo-strato", label: "STRATO Postfach" })} />}
        {accounts.filter((account) => !hiddenAccounts.includes(account.id)).length === 0 && (!fakeAccountVisible || accounts.length > 0) && <EmptyConnectionCard text="Noch kein Postfach verbunden." />}
      </ConnectionSection></div>
      <div data-section-id="settings-model"><ConnectionSection title="KI-Modelle" tooltip="Lokale KI-Modelle werden über Ollama verbunden. Sie bleiben auf diesem Gerät und können nach einem Fähigkeitstest für unklare E-Mails eingesetzt werden." count={fakeModelVisible ? 1 : 0} add={<Button size="icon-sm" aria-label="KI-Modell hinzufügen"><Plus /></Button>}>
        {fakeModelVisible ? <ConnectionCard icon={Bot} title="Qwen3 4B" detail="Ollama · lokal verbunden" enabled={connectionEnabled["demo-qwen"]} onEnabled={(enabled) => setConnectionEnabled((current) => ({ ...current, "demo-qwen": enabled }))} onSettings={() => {}} onDelete={() => setDeleteTarget({ kind: "model", id: "demo-qwen", label: "Qwen3 4B" })} /> : <EmptyConnectionCard text="Noch kein KI-Modell verbunden." />}
      </ConnectionSection></div>
      <div data-section-id="settings-security" className="flex gap-3 border-t border-white/[0.09] pt-6"><ShieldCheck className="mt-0.5 size-4 shrink-0 text-[#999]" /><div><p className="text-sm font-medium">Sicherheit</p><p className="mt-1 text-xs leading-5 text-[#777]">Passwörter liegen im Betriebssystem-Schlüsselbund. Nachrichtentexte werden nicht dauerhaft gespeichert.</p></div></div>
    </section>
    </div></div>
    <ScrollFade strength={settingsFade} targetRef={settingsScrollRef} />
    <SectionIndicator items={settingsSections} scrollRef={settingsScrollRef} />
    <FloatingActions visible={editingFolder} primary="Speichern" disabled={!folderDraft.trim()} onPrimary={() => { setFolderName(folderDraft.trim()); setEditingFolder(false) }} onCancel={() => { setFolderDraft(folderName); setEditingFolder(false) }} />
    <FloatingActions visible={Boolean(deleteTarget)} primary="Wirklich löschen?" onPrimary={() => { if (!deleteTarget) return; if (deleteTarget.kind === "model") setFakeModelVisible(false); else if (deleteTarget.id === "demo-strato") setFakeAccountVisible(false); else setHiddenAccounts((current) => [...current, deleteTarget.id]); setDeleteTarget(null) }} onCancel={() => setDeleteTarget(null)} />
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

function TransferPlaceholder({ kind }: { kind: "learning" | "profile" }) {
  const [open, setOpen] = useState(false)
  const learning = kind === "learning"
  return <><Tooltip><TooltipTrigger render={<Button size="sm" variant="outline" onClick={() => setOpen(true)}>{learning ? "Lerntransfer" : "Profiltransfer"}</Button>} /><TooltipContent side="top">{learning ? "Anonymisierte Lernmerkmale übertragen" : "Vollständiges Lernprofil übertragen"}</TooltipContent></Tooltip><Dialog open={open} onOpenChange={setOpen}><DialogContent className="border-white/[0.08] bg-[#1d1d1d] sm:max-w-[520px]"><DialogHeader><DialogTitle>{learning ? "Lerndaten übertragen" : "Profil übertragen"}</DialogTitle><DialogDescription>{learning ? "Überträgt ausschließlich allgemeine, anonymisierte Lernmerkmale ohne Nachrichtentexte oder personenbezogene Daten." : "Überträgt Regeln, Präferenzen und postfachspezifische Lernmerkmale in ein anderes Profil."}</DialogDescription></DialogHeader><div className="space-y-3 py-2"><Label htmlFor={`${kind}-target`}>Zielprofil</Label><Input id={`${kind}-target`} className="h-12 px-3.5" placeholder="Profile durchsuchen" /><div className="rounded-[10px] border border-dashed border-white/10 px-4 py-5 text-center text-xs text-[#666]">Die Profilauswahl und Übertragung werden später angebunden.</div></div><DialogFooter><Button variant="outline" onClick={() => setOpen(false)}>Schließen</Button></DialogFooter></DialogContent></Dialog></>
}

function ConnectionCard({ icon: Icon, title, detail, enabled, onEnabled, onSettings, onDelete, children }: { icon: typeof Inbox; title: string; detail: string; enabled: boolean; onEnabled: (enabled: boolean) => void; onSettings: () => void; onDelete?: () => void; children?: React.ReactNode }) {
  return <div className="rounded-[10px] border border-white/10 bg-[#202020] p-4"><div className="flex items-center gap-3"><div className="flex size-9 shrink-0 items-center justify-center rounded-md bg-white/[0.04] text-[#999]"><Icon className="size-4" /></div><div className="min-w-0 flex-1"><p className="truncate text-sm font-medium">{title}</p><p className="mt-1 truncate text-xs text-[#666]">{detail}</p></div><div className="flex shrink-0 items-center gap-0.5"><button className="flex size-8 items-center justify-center rounded-md text-[#666] transition-colors hover:bg-white/[0.05] hover:text-white" onClick={onSettings} aria-label={`${title} verwalten`}><Settings className="size-4" /></button>{onDelete && <button className="flex size-8 items-center justify-center rounded-md text-[#666] transition-colors hover:bg-white/[0.05] hover:text-white" onClick={onDelete} aria-label={`${title} löschen`}><Trash2 className="size-4" /></button>}<CompactOnOff enabled={enabled} onChange={onEnabled} label={title} /></div></div>{children}</div>
}

function CompactOnOff({ enabled, onChange, label }: { enabled: boolean; onChange: (enabled: boolean) => void; label: string }) {
  return <div className="ml-2 flex h-8 items-center rounded-md border border-white/10 bg-[#171717] p-1" role="group" aria-label={`${label} ein- oder ausschalten`}><button className={`h-full rounded-sm px-2 text-[10px] font-medium transition-colors ${enabled ? "bg-white text-[#171717]" : "text-[#666] hover:text-white"}`} onClick={() => onChange(true)} aria-pressed={enabled}>An</button><button className={`h-full rounded-sm px-2 text-[10px] font-medium transition-colors ${!enabled ? "bg-white text-[#171717]" : "text-[#666] hover:text-white"}`} onClick={() => onChange(false)} aria-pressed={!enabled}>Aus</button></div>
}

function EmptyConnectionCard({ text }: { text: string }) {
  return <div className="flex min-h-20 items-center rounded-[10px] border border-dashed border-white/10 bg-white/[0.015] px-4 text-sm text-[#666]">{text}</div>
}

function AddAccount({ refresh }: { refresh: () => void }) {
  const [open, setOpen] = useState(false)
  const [step, setStep] = useState(0)
  const [saving, setSaving] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState("")
  const [preferences, setPreferences] = useState({ customers: true, suppliers: true, newsletters: true, automatedAccounts: true })
  const [form, setForm] = useState({ name: "STRATO", host: "imap.strato.de", port: "993", username: "", password: "", purpose: "", industry: "", languages: "Deutsch", whitelist: "", context: "" })
  const steps = ["Verbindung", "Profil", "Regeln", "Prüfen"]
  const trustedSenders = form.whitelist.split(/[\n,;]/).map((value) => value.trim()).filter(Boolean)
  const save = async () => { setSaving(true); setError(""); try { await agentRequest("POST", "/v1/accounts", { account: { name: form.name, host: form.host, port: Number(form.port) || 993, username: form.username, inboxFolder: "INBOX", sentFolder: "Sent", spamFolder: "AI_SPAM_FILTER", safetyMode: "safe", enabled: true, dryRun: true, ollamaValidated: false, profile: { purpose: [form.purpose, form.context].filter(Boolean).join(" · "), industry: form.industry, languages: form.languages.split(/[,;]/).map((value) => value.trim()).filter(Boolean), expectedMailTypes: [preferences.customers && "Kundenanfragen", preferences.suppliers && "Lieferanten", preferences.newsletters && "Newsletter", preferences.automatedAccounts && "Automatische Kontomails"].filter(Boolean), trustedDomains: [], trustedSenders, wantedNewsletters: preferences.newsletters ? ["Erwünschte Newsletter"] : [], legitimateAutomated: preferences.automatedAccounts ? ["Konten und Portale"] : [] } }, password: form.password }); setOpen(false); setStep(0); await refresh() } catch (reason) { setError(reason instanceof Error ? reason.message : "Postfach konnte nicht gespeichert werden") } finally { setSaving(false) } }
  return <Dialog open={open} onOpenChange={(nextOpen) => { setOpen(nextOpen); if (!nextOpen) setStep(0) }}><DialogTrigger render={<Button size="icon-sm" aria-label="Postfach hinzufügen"><Plus /></Button>} /><DialogContent className="max-h-[88vh] overflow-y-auto border-white/[0.08] bg-[#1d1d1d] p-6 sm:max-w-[720px]"><DialogHeader><DialogTitle>Postfach verbinden</DialogTitle><DialogDescription>Schritt {step + 1} von {steps.length} · {steps[step]}</DialogDescription></DialogHeader><div className="grid grid-cols-4 gap-2 py-2">{steps.map((label, index) => <div key={label}><div className={`h-1 rounded-full ${index <= step ? "bg-white" : "bg-white/10"}`} /><p className={`mt-2 text-[11px] ${index === step ? "text-white" : "text-[#666]"}`}>{label}</p></div>)}</div><div className="min-h-[340px] py-3">
    {step === 0 && <div className="grid gap-5"><Field label="Name des Postfachs"><Input className="h-12 px-3.5" placeholder="Zum Beispiel STRATO Geschäftlich" value={form.name} onChange={(e) => setForm({...form,name:e.target.value})} /><p className="mt-1 text-[11px] text-[#666]">Dieser Name erscheint später auf der Postfach-Card.</p></Field><div className="grid grid-cols-[1fr_160px] gap-4"><Field label="IMAP-Server"><Input className="h-12 px-3.5" value={form.host} onChange={(e) => setForm({...form,host:e.target.value})} /></Field><Field label="Port"><Input className="h-12 px-3.5" inputMode="numeric" value={form.port} onChange={(e) => setForm({...form,port:e.target.value})} /></Field></div><Field label="E-Mail / Benutzername"><Input className="h-12 px-3.5" placeholder="name@beispiel.de" value={form.username} onChange={(e) => setForm({...form,username:e.target.value})} /></Field><Field label="App-Passwort"><div className="relative"><Input className="h-12 px-3.5 pr-12" type={showPassword ? "text" : "password"} value={form.password} onChange={(e) => setForm({...form,password:e.target.value})} /><button type="button" className="absolute right-3.5 top-1/2 flex size-5 -translate-y-1/2 items-center justify-center text-[#777] transition-colors hover:text-white" onClick={() => setShowPassword((visible) => !visible)} aria-label={showPassword ? "Passwort ausblenden" : "Passwort anzeigen"}>{showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}</button></div></Field><p className="text-xs leading-5 text-[#666]">Die Zugangsdaten werden im Schlüsselbund des Betriebssystems gespeichert. Die Ersteinrichtung beginnt im Trockenlauf.</p></div>}
    {step === 1 && <div className="grid gap-5"><Field label="Branche (optional)"><Input className="h-12 px-3.5" placeholder="Zum Beispiel Handwerk" value={form.industry} onChange={(e) => setForm({...form,industry:e.target.value})} /></Field><Field label="Zweck des Postfachs (optional)"><Textarea className="min-h-28 px-3.5 py-3" placeholder="Zum Beispiel: Kundenanfragen, Lieferanten und Rechnungen eines Fliesenlegerbetriebs" value={form.purpose} onChange={(e) => setForm({...form,purpose:e.target.value})} /></Field><Field label="Erwartete Sprachen (optional)"><Input className="h-12 px-3.5" placeholder="Deutsch, Englisch" value={form.languages} onChange={(e) => setForm({...form,languages:e.target.value})} /></Field></div>}
    {step === 2 && <div className="grid gap-5"><div className="flex items-center gap-2"><p className="text-sm font-medium">Was gehört normalerweise in dieses Postfach?</p><InfoTooltip><p>Alle Angaben sind optional. Je mehr legitime Nachrichtentypen bekannt sind, desto besser lassen sich Fehlalarme vermeiden.</p></InfoTooltip></div><div className="grid grid-cols-2 gap-3">{([['customers','Kundenanfragen','Anfragen, Angebote und Rückfragen'],['suppliers','Lieferanten','Bestellungen, Versand und Rechnungen'],['newsletters','Newsletter','Erwünschte Newsletter berücksichtigen'],['automatedAccounts','Konten und Portale','Logins, Bestätigungen und Systemmails']] as const).map(([key,title,detail]) => <PreferenceCard key={key} title={title} detail={detail} enabled={preferences[key]} onEnabled={(enabled) => setPreferences((current) => ({ ...current, [key]: enabled }))} />)}</div><Field label="Whitelist (optional)"><Textarea className="min-h-24 px-3.5 py-3" placeholder={'Eine E-Mail-Adresse pro Zeile\nlieferant@beispiel.de\nkunde@firma.de'} value={form.whitelist} onChange={(e) => setForm({...form,whitelist:e.target.value})} /></Field><Field label="Weitere Beschreibung (optional)"><Textarea className="min-h-20 px-3.5 py-3" placeholder="Beschreibe kurz ungewöhnliche, aber legitime E-Mails." value={form.context} onChange={(e) => setForm({...form,context:e.target.value})} /></Field></div>}
    {step === 3 && <div className="space-y-4"><div className="rounded-[10px] border border-white/10 bg-[#202020] p-4"><p className="text-sm font-medium">{form.name || "Postfach"}</p><p className="mt-1 text-xs text-[#666]">{form.username} · {form.host}:{form.port}</p></div><div className="grid grid-cols-2 gap-3 text-xs"><div className="rounded-[10px] border border-white/10 p-4"><p className="text-[#666]">Profil</p><p className="mt-2 leading-5">{form.industry || "Keine Branche"}<br />{form.languages || "Keine Sprache"}</p></div><div className="rounded-[10px] border border-white/10 p-4"><p className="text-[#666]">Whitelist</p><p className="mt-2 leading-5">{trustedSenders.length} bestätigte Absender</p></div></div><p className="text-xs leading-5 text-[#666]">Nach dem Verbinden wird ausschließlich lesend geprüft. Automatische Verschiebungen bleiben deaktiviert, bis der Trockenlauf bestätigt wurde.</p>{error && <p role="alert" className="rounded-lg border border-white/[0.08] bg-white/[0.03] p-3 text-xs text-[#bbb]">{error}</p>}</div>}
  </div><DialogFooter className="border-t border-white/[0.09] pt-4"><Button variant="ghost" onClick={() => step === 0 ? setOpen(false) : setStep((current) => current - 1)}>{step === 0 ? "Abbrechen" : "Zurück"}</Button>{step < steps.length - 1 ? <Button disabled={step === 0 && (!form.host || !form.username || !form.password)} onClick={() => setStep((current) => current + 1)}>Weiter</Button> : <Button disabled={saving} onClick={() => void save()}>{saving ? "Verbindet …" : "Sicher verbinden"}</Button>}</DialogFooter></DialogContent></Dialog>
}

function PreferenceCard({ title, detail, enabled, onEnabled }: { title: string; detail: string; enabled: boolean; onEnabled: (enabled: boolean) => void }) {
  return <div className="flex min-h-24 items-start gap-3 rounded-[10px] border border-white/10 bg-[#202020] p-4"><div className="min-w-0 flex-1"><p className="text-sm font-medium">{title}</p><p className="mt-1 text-xs leading-5 text-[#666]">{detail}</p></div><Switch checked={enabled} onCheckedChange={onEnabled} /></div>
}

function Field({ label, children }: { label: string; children: React.ReactNode }) { return <div className="space-y-2"><Label>{label}</Label>{children}</div> }
