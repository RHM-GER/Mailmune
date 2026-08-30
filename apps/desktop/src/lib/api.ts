import { invoke } from "@tauri-apps/api/core"

export type SafetyMode = "confirm_all" | "safe" | "aggressive"
export type DecisionStatus = "pending" | "moved" | "confirmed" | "rejected" | "deferred"

export interface Evidence {
  group: string
  code: string
  weight: number
  summary: string
}

export interface Decision {
  id: string
  accountId: string
  originFolder: string
  currentFolder: string
  from: string
  subject: string
  score: number
  status: DecisionStatus
  evidence: Evidence[]
  receivedAt: string
}

export interface Summary {
  accounts: number
  pending: number
  moved: number
  confirmed: number
  rejected: number
  falsePositiveRate: number
  processedWeek: number
}

export interface Account {
  id: string
  name: string
  host: string
  port: number
  username: string
  secretRef: string
  inboxFolder: string
  sentFolder: string
  spamFolder: string
  safetyMode: SafetyMode
  ollamaModel?: string
  ollamaValidated: boolean
  enabled: boolean
  dryRun: boolean
  profile: {
    purpose: string
    industry: string
    languages: string[]
    expectedMailTypes: string[]
    trustedDomains: string[]
    trustedSenders: string[]
    wantedNewsletters: string[]
    legitimateAutomated: string[]
  }
}

export function isTauri() {
  return "__TAURI_INTERNALS__" in window
}

export async function agentRequest<T>(method: string, path: string, body?: unknown): Promise<T> {
  if (!isTauri()) throw new Error("Agent ist nur in der Desktop-App erreichbar")
  return invoke<T>("agent_request", { method, path, body: body ?? null })
}

export const demoSummary: Summary = {
  accounts: 1,
  pending: 12,
  moved: 86,
  confirmed: 74,
  rejected: 2,
  falsePositiveRate: 0.026,
  processedWeek: 438,
}

export const demoDecisions: Decision[] = [
  { id: "1", accountId: "strato", originFolder: "INBOX", currentFolder: "AI_SPAM_FILTER", from: "angebote@bonus-center.example", subject: "Letzte Chance: Bonus jetzt bestätigen", score: 0.99, status: "moved", evidence: [{ group: "content", code: "pressure_language", weight: 0.35, summary: "Druck- oder Lockformulierung erkannt" }, { group: "links", code: "suspicious_links", weight: 0.35, summary: "Verkürzte Links erkannt" }], receivedAt: new Date(Date.now() - 42 * 60_000).toISOString() },
  { id: "2", accountId: "strato", originFolder: "INBOX", currentFolder: "INBOX", from: "billing@unknown-pay.example", subject: "Zahlung fehlgeschlagen – sofort handeln", score: 0.96, status: "pending", evidence: [{ group: "content", code: "pressure_language", weight: 0.35, summary: "Ungewöhnlicher Handlungsdruck" }], receivedAt: new Date(Date.now() - 3 * 3_600_000).toISOString() },
  { id: "3", accountId: "strato", originFolder: "INBOX", currentFolder: "AI_SPAM_FILTER", from: "newsletter@tool-shop.example", subject: "Werkzeugangebote im August", score: 0.81, status: "confirmed", evidence: [{ group: "mailing_list", code: "list_unsubscribe", weight: -0.08, summary: "Mailinglisten-Kopfzeile vorhanden" }, { group: "model", code: "local_model_spam", weight: 0.87, summary: "Lokales Modell bewertet die Mail als Spam" }], receivedAt: new Date(Date.now() - 26 * 3_600_000).toISOString() },
  ...Array.from({ length: 14 }, (_, index): Decision => ({
    id: `preview-${index + 4}`,
    accountId: "strato",
    originFolder: "INBOX",
    currentFolder: index % 4 === 0 || index % 4 === 2 ? "AI_SPAM_FILTER" : "INBOX",
    from: index % 2 === 0 ? `versand-${index + 1}@promo-center.example` : `service-${index + 1}@unknown-shop.example`,
    subject: index % 2 === 0 ? "Exklusives Angebot nur für kurze Zeit" : "Ihr Konto benötigt eine Bestätigung",
    score: [0.79, 0.72, 0.65, 0.58, 0.5, 0.42, 0.35, 0.28, 0.2, 0.15, 0.1, 0.05, 0.02, 0][index],
    status: (["moved", "pending", "confirmed", "rejected"] as const)[index % 4],
    evidence: [{ group: "content", code: "preview_signal", weight: 0.32, summary: index % 2 === 0 ? "Ungewöhnliche Angebotsformulierung" : "Unbekannter Absender mit Handlungsdruck" }],
    receivedAt: new Date(Date.now() - (index + 5) * 4_200_000).toISOString(),
  })),
]
