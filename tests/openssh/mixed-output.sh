#!/usr/bin/env bash
set -euo pipefail
duration="${1:-3000}"
[[ "$duration" =~ ^[0-9]+$ ]] && ((duration >= 10 && duration <= 7200))
mapfile -t terminals < <(ps -u yaaretest -o tty= | awk '$1 ~ /^pts\/[0-9]+$/ {print "/dev/" $1}' | sort -u)
((${#terminals[@]} >= 1 && ${#terminals[@]} <= 8))
printf 'Native soak output: %s PTYs, %s seconds.\n' "${#terminals[@]}" "$duration"
end=$((SECONDS+duration))
batch=0
while ((SECONDS < end)); do
  for terminal in "${terminals[@]}"; do
    [[ -w "$terminal" ]] || { printf 'Original native PTY disappeared.\n' >&2; exit 1; }
    if ((batch % 60 < 10)); then
      awk -v b="$batch" 'BEGIN {for(i=0;i<128;i++) printf "SOAK_%06d_%03d ASCII 0123456789 \033[32mUTF-8 中文\033[0m \033[34mblue\033[0m\r\n",b,i}' > "$terminal"
    else
      printf 'SOAK_HEARTBEAT_%06d\r\n' "$batch" > "$terminal"
    fi
  done
  batch=$((batch+1))
  sleep 0.5
done
for terminal in "${terminals[@]}"; do printf '\r\nYAARE_SOAK_FINISHED_%s\r\n' "$batch" > "$terminal"; done
printf 'Finished %s batches on %s original PTYs.\n' "$batch" "${#terminals[@]}"
