// Network ops dashboard — 9 sites, all checks in parallel
(
  pfmap "ssl_check" "github.com,anthropic.com,google.com,openai.com,cloudflare.com,amazon.com,microsoft.com,apple.com,stripe.com"
  <*> pfmap "ping" "github.com,anthropic.com,google.com,openai.com,cloudflare.com,amazon.com,microsoft.com,apple.com,stripe.com"
  <*> pfmap "http_check" "https://github.com,https://anthropic.com,https://google.com,https://openai.com,https://cloudflare.com,https://amazon.com,https://microsoft.com,https://apple.com,https://stripe.com"
  <*> pfmap "dns_lookup" "github.com,anthropic.com,google.com,openai.com,cloudflare.com,amazon.com,microsoft.com,apple.com,stripe.com"
)
>=> merge
>=> <>
>=> ask "Parse all this network diagnostic data and convert to a JSON object with this exact structure:
{
  'sites': [
    {
      'domain': 'github.com',
      'ssl_status': 'VALID',
      'ssl_issuer': 'Sectigo',
      'ssl_days_remaining': 73,
      'ssl_tls_version': 'TLS 1.3',
      'ssl_valid_until': '2026-06-03',
      'ping_status': 'REACHABLE',
      'ping_latency_ms': 12,
      'http_status_code': 200,
      'http_latency_ms': 145,
      'http_final_url': 'https://github.com',
      'dns_a_records': '140.82.121.4',
      'overall_health': 'HEALTHY'
    }
  ],
  'checked_at': '2026-03-22T10:00:00Z'
}
overall_health: HEALTHY if all checks pass, DEGRADED if any warning, DOWN if unreachable or cert expired.
Output ONLY raw JSON, no markdown, no backticks."
>=> ask "Build a single HTML file network operations dashboard. Requirements:
- Output ONLY raw HTML starting with <!DOCTYPE html>, no markdown, no backticks, no code fences
- Embedded React via CDN babel standalone
- DataTables.js with full filtering, sorting, pagination, global search box, column filters
- Dark theme, modern professional UI with gradient header
- Five tabs: Overview, SSL Certs, Ping, HTTP, DNS
- Overview tab: summary cards (Total Sites, Healthy, Degraded, Down) + master DataTable with all metrics
- SSL tab: Domain, Status, Issuer, Valid Until, Days Remaining, TLS Version — color rows red/orange/yellow/green
- Ping tab: Domain, Status, Latency ms — color green/red
- HTTP tab: Domain, Status Code, Latency ms, Final URL — color by status code
- DNS tab: Domain, A Records, MX, NS Records
- Last checked timestamp in header
- Responsive design
Output ONLY raw HTML starting with <!DOCTYPE html>."
>=> save "network-dashboard.html"
