#!/usr/bin/env bash
set -euo pipefail
batches="${1:-400}"
[[ "$batches" =~ ^[0-9]+$ ]] && ((batches >= 1 && batches <= 1200))
mapfile -t terminals < <(ps -u yaaretest -o tty= | awk '$1 ~ /^pts\/[0-9]+$/ {print "/dev/" $1}' | sort -u)
((${#terminals[@]} >= 1 && ${#terminals[@]} <= 8))
for ((batch=0;batch<batches;batch++)); do
  for terminal in "${terminals[@]}"; do
    awk -v b="$batch" 'BEGIN { for (i=0;i<256;i++) printf "YAARE_STRESS_%06d_%03d 0123456789 abcdefghijklmnopqrstuvwxyz 0123456789 abcdefghijklmnopqrstuvwxyz \033[32mUTF-8 中文\033[0m\r\n",b,i }' > "$terminal"
  done
  sleep 0.05
done
for terminal in "${terminals[@]}"; do printf '\r\nYAARE_STRESS_FINISHED\r\n' > "$terminal"; done
printf 'Completed %s batches for %s isolated PTYs.\n' "$batches" "${#terminals[@]}"
