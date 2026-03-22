// Step 1: collect all network data
(
  pfmap "ssl_check" "github.com,anthropic.com,google.com,openai.com,cloudflare.com,amazon.com,microsoft.com,apple.com,stripe.com"
  <*> pfmap "ping" "github.com,anthropic.com,google.com,openai.com,cloudflare.com,amazon.com,microsoft.com,apple.com,stripe.com"
  <*> pfmap "http_check" "https://github.com,https://anthropic.com,https://google.com,https://openai.com,https://cloudflare.com,https://amazon.com,https://microsoft.com,https://apple.com,https://stripe.com"
  <*> pfmap "dns_lookup" "github.com,anthropic.com,google.com,openai.com,cloudflare.com,amazon.com,microsoft.com,apple.com,stripe.com"
)
>=> merge
>=> <>
>=> claude "Parse this network data into JSON: { sites: [ { domain, ssl_status, ssl_issuer, ssl_days_remaining, ssl_tls_version, ssl_valid_until, ping_status, ping_latency_ms, http_status_code, http_latency_ms, http_final_url, dns_a_records, overall_health } ], checked_at }. overall_health: HEALTHY/DEGRADED/DOWN. Raw JSON only."
>=> save "network-data.json"
