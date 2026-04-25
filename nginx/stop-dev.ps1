$repoRoot = Split-Path -Parent $PSScriptRoot

& nginx -p $repoRoot -c "nginx/dev.conf" -s stop
