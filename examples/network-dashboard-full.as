// ============================================================================
// Network Health Dashboard — full end-to-end pipeline
// ============================================================================

// ── Stage 1 & 2: parallel probe → fold ──────────────────────────────────────

(
  pfmap "ssl_check"  "github.com,anthropic.com,google.com,openai.com,cloudflare.com,amazon.com,microsoft.com,apple.com,stripe.com,linkedin.com,twitter.com,reddit.com"
  <*>
  pfmap "ping"       "github.com,anthropic.com,google.com,openai.com,cloudflare.com,amazon.com,microsoft.com,apple.com,stripe.com,linkedin.com,twitter.com,reddit.com"
  <*>
  pfmap "http_check" "https://github.com,https://anthropic.com,https://google.com,https://openai.com,https://cloudflare.com,https://amazon.com,https://microsoft.com,https://apple.com,https://stripe.com,https://linkedin.com,https://twitter.com,https://reddit.com"
  <*>
  pfmap "dns_lookup" "github.com,anthropic.com,google.com,openai.com,cloudflare.com,amazon.com,microsoft.com,apple.com,stripe.com,linkedin.com,twitter.com,reddit.com"
)
>=> merge
>=> <>
>=> claude "Parse ALL of this network telemetry into a single JSON object. Output raw JSON only, no markdown, no backticks, no explanation. Schema: { checked_at: ISO8601 string, sites: [ { domain, ssl_status: valid|expired|error, ssl_issuer, ssl_days_remaining: int, ssl_tls_version, ssl_valid_until: YYYY-MM-DD, ping_status: reachable|unreachable, ping_latency_ms: int, http_status_code: string, http_latency_ms: int, http_final_url, dns_a_records, overall_health: HEALTHY|DEGRADED|DOWN } ] }. Rules: DOWN if ping unreachable or http error; DEGRADED if ssl_days_remaining<30 or http_latency_ms>500; HEALTHY otherwise."
>=> save "network-data.json"

// ── Stage 3: render dashboard immediately after save ─────────────────────────
// render reads network-data.json directly from disk — no pipe dependency

render "examples/network.table" "network-data.json" "network-dashboard.html"

// ── Stage 4: health triage — read JSON, match, alert ─────────────────────────
// read is a fresh top-level statement; no file gets moved

read "network-data.json"
>=> match
  | contains "DOWN"     >=> claude "Summarise which sites are DOWN and why, in 3 bullet points." >=> notify "slack"
  | contains "DEGRADED" >=> claude "Summarise which sites are DEGRADED (SSL expiry or high latency) in 3 bullet points." >=> notify "slack"
  | _                   >=> ask "Confirm in one sentence that all monitored sites are healthy."
