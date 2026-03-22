pfmap "ssl_check" "github.com,anthropic.com,openai.com,google.com"
>=> <>
>=> ask "Does the output contain EXPIRED, CRITICAL or WARNING? Answer only YES or NO."
>=> match "| contains YES >=> notify slack
| _ >=> save ssl-all-ok.md"
