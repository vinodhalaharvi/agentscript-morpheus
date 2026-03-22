pfmap "ssl_check" "github.com,anthropic.com,openai.com,google.com"
>=> <>
>=> match
  | contains "EXPIRED"   >=> notify "slack"
  | contains "CRITICAL"  >=> notify "slack"
  | contains "WARNING"   >=> notify "slack"
  | _                    >=> save "ssl-all-ok.md"
