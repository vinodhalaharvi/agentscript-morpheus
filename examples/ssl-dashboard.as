pfmap "ssl_check" "github.com,anthropic.com,openai.com,google.com"
>=> <>
>=> ask "Convert this SSL check data into a JSON array. Each item must have these exact fields: domain, status, issuer, validFrom, validUntil, daysRemaining, tlsVersion. Only output raw JSON array, no markdown, no backticks, no explanation."
>=> ask "Build a single HTML file SSL certificate dashboard. Requirements:
- No markdown, no backticks, no code fences, output ONLY raw HTML starting with <!DOCTYPE html>
- Embedded React via CDN babel standalone
- DataTables.js with full filtering, sorting, pagination, search box
- Dark theme with modern UI
- Table columns: Domain, Status, Issuer, Valid From, Valid Until, Days Remaining, TLS Version
- Color code rows: red if EXPIRED, orange if under 7 days, yellow if under 30 days, green if healthy
- Summary cards at top: Total Certs, Healthy, Warning, Critical counts
- Last checked timestamp
- Sort by days remaining ascending by default
Output ONLY the raw HTML. Start with <!DOCTYPE html> and end with </html>."
>=> save "ssl-dashboard.html"
