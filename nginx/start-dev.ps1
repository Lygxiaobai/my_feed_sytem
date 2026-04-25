$repoRoot = Split-Path -Parent $PSScriptRoot
$runtimeDir = Join-Path $repoRoot "nginx\runtime"

New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $runtimeDir "client_body_temp") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $runtimeDir "proxy_temp") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $runtimeDir "fastcgi_temp") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $runtimeDir "uwsgi_temp") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $runtimeDir "scgi_temp") | Out-Null

Start-Process -FilePath "nginx" -ArgumentList "-p `"$repoRoot`" -c `"nginx/dev.conf`"" -WindowStyle Hidden
