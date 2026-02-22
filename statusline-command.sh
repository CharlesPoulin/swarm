#!/usr/bin/env bash
# Claude Code status line: persistent token usage bar (mirrors /usage output)

input=$(cat)

used_pct=$(echo "$input" | jq -r '.context_window.used_percentage // empty')
remaining_pct=$(echo "$input" | jq -r '.context_window.remaining_percentage // empty')
total_input=$(echo "$input" | jq -r '.context_window.total_input_tokens // 0')
window_size=$(echo "$input" | jq -r '.context_window.context_window_size // 0')
model=$(echo "$input" | jq -r '.model.display_name // "Claude"')

# If no messages yet, show idle state
if [ -z "$used_pct" ]; then
  printf "%s | Context: no messages yet" "$model"
  exit 0
fi

# Build a 20-character bar
bar_width=20
used_int=$(printf "%.0f" "$used_pct")
filled=$(( used_int * bar_width / 100 ))
empty=$(( bar_width - filled ))

bar=""
for i in $(seq 1 $filled); do bar="${bar}█"; done
for i in $(seq 1 $empty);  do bar="${bar}░"; done

# Color the bar: green < 70%, yellow < 90%, red >= 90%
if [ "$used_int" -ge 90 ]; then
  color="\033[31m"   # red
elif [ "$used_int" -ge 70 ]; then
  color="\033[33m"   # yellow
else
  color="\033[32m"   # green
fi
reset="\033[0m"

# Format token counts with k suffix for readability
fmt_tokens() {
  local n=$1
  if [ "$n" -ge 1000 ]; then
    printf "%dk" $(( n / 1000 ))
  else
    printf "%d" "$n"
  fi
}

used_tokens=$(fmt_tokens "$total_input")
window_tokens=$(fmt_tokens "$window_size")

printf "${color}[%s]${reset} %s%% used (%s / %s tokens) | %s%% remaining" \
  "$bar" "$used_int" "$used_tokens" "$window_tokens" \
  "$(printf "%.0f" "${remaining_pct:-0}")"
