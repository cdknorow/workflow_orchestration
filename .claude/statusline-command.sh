#!/usr/bin/env bash

input=$(cat)

# Extract fields from JSON input
cwd=$(echo "$input" | jq -r '.workspace.current_dir // .cwd // empty')
model=$(echo "$input" | jq -r '.model.display_name // empty')
used_pct=$(echo "$input" | jq -r '.context_window.used_percentage // empty')

# Token breakdown from current_usage (null-safe)
in_tokens=$(echo "$input" | jq -r '.context_window.current_usage.input_tokens // empty')
out_tokens=$(echo "$input" | jq -r '.context_window.current_usage.output_tokens // empty')
cache_read=$(echo "$input" | jq -r '.context_window.current_usage.cache_read_input_tokens // empty')

# Git branch (skip optional locks)
branch=""
if git -C "$cwd" rev-parse --git-dir > /dev/null 2>&1; then
    branch=$(git -C "$cwd" --no-optional-locks symbolic-ref --short HEAD 2>/dev/null)
fi

# Worktree: basename of current dir
worktree=$(basename "$cwd")

# Build status parts
parts=()

if [ -n "$worktree" ]; then
    parts+=("worktree:$worktree")
fi

if [ -n "$branch" ]; then
    parts+=("branch:$branch")
fi

if [ -n "$model" ]; then
    parts+=("model:$model")
fi

if [ -n "$used_pct" ]; then
    # Round to integer
    used_int=$(printf "%.0f" "$used_pct" 2>/dev/null || echo "$used_pct")
    parts+=("ctx:${used_int}%")
fi

if [ -n "$in_tokens" ] && [ -n "$out_tokens" ]; then
    parts+=("in:${in_tokens} out:${out_tokens}")
fi

if [ -n "$cache_read" ] && [ "$cache_read" -gt 0 ] 2>/dev/null; then
    parts+=("cache:${cache_read}")
fi

# Join parts with " | "
output=""
for part in "${parts[@]}"; do
    if [ -z "$output" ]; then
        output="$part"
    else
        output="$output | $part"
    fi
done

printf "%s" "$output"
