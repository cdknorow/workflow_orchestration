/* @file mention autocomplete for the command input textarea */

import { state } from './state.js';

let dropdown = null;
let selectedIndex = 0;
let currentResults = [];
let mentionStart = -1; // position of the '@' character
let debounceTimer = null;

export function initFileMention() {
    const input = document.getElementById("command-input");
    if (!input) return;

    // Create dropdown element
    dropdown = document.createElement("div");
    dropdown.className = "file-mention-dropdown";
    dropdown.style.display = "none";
    // Append to the command-editor wrapper so it positions relative to the textarea
    input.parentElement.appendChild(dropdown);

    input.addEventListener("input", onInput);
    input.addEventListener("keydown", onKeydown);
    input.addEventListener("blur", () => {
        // Delay hide so click on dropdown item can fire first
        setTimeout(hideDropdown, 200);
    });
}

function onInput(e) {
    const input = e.target;
    const text = input.value;
    const cursor = input.selectionStart;

    // Find the '@' that starts the current mention
    const beforeCursor = text.slice(0, cursor);
    const atIndex = findMentionStart(beforeCursor);

    if (atIndex === -1) {
        hideDropdown();
        return;
    }

    mentionStart = atIndex;
    const query = beforeCursor.slice(atIndex + 1); // text after '@'

    // Debounce the API call
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => fetchFiles(query), 150);
}

function findMentionStart(text) {
    // Walk backwards from end to find '@' that isn't preceded by a non-whitespace char
    // (so "foo@bar" doesn't trigger, but "foo @bar" or "@bar" does)
    for (let i = text.length - 1; i >= 0; i--) {
        const ch = text[i];
        // If we hit whitespace or newline before finding '@', no active mention
        if (ch === ' ' || ch === '\n' || ch === '\t') return -1;
        if (ch === '@') {
            // Valid if at start of text or preceded by whitespace
            if (i === 0 || /\s/.test(text[i - 1])) return i;
            return -1;
        }
    }
    return -1;
}

async function fetchFiles(query) {
    if (!state.currentSession || state.currentSession.type !== "live") {
        hideDropdown();
        return;
    }

    const name = encodeURIComponent(state.currentSession.name);
    const params = new URLSearchParams({ q: query });
    if (state.currentSession.session_id) {
        params.set("session_id", state.currentSession.session_id);
    }

    try {
        const resp = await fetch(`/api/sessions/live/${name}/search-files?${params}`);
        if (!resp.ok) { hideDropdown(); return; }
        const data = await resp.json();
        currentResults = data.files || [];
        if (currentResults.length === 0) {
            hideDropdown();
            return;
        }
        selectedIndex = 0;
        renderDropdown(query);
    } catch {
        hideDropdown();
    }
}

function renderDropdown(query) {
    if (!dropdown || currentResults.length === 0) {
        hideDropdown();
        return;
    }

    dropdown.innerHTML = currentResults.map((fp, i) => {
        const cls = i === selectedIndex ? "file-mention-item selected" : "file-mention-item";
        const highlighted = highlightMatch(fp, query);
        return `<div class="${cls}" data-index="${i}">${highlighted}</div>`;
    }).join("");

    dropdown.style.display = "block";

    // Add click handlers
    dropdown.querySelectorAll(".file-mention-item").forEach(el => {
        el.addEventListener("mousedown", (e) => {
            e.preventDefault(); // prevent blur
            const idx = parseInt(el.dataset.index);
            selectItem(idx);
        });
    });
}

function highlightMatch(filepath, query) {
    if (!query) return escapeHtml(filepath);
    const lower = filepath.toLowerCase();
    const qLower = query.toLowerCase();
    const idx = lower.indexOf(qLower);
    if (idx === -1) return escapeHtml(filepath);

    const before = filepath.slice(0, idx);
    const match = filepath.slice(idx, idx + query.length);
    const after = filepath.slice(idx + query.length);
    return `${escapeHtml(before)}<strong>${escapeHtml(match)}</strong>${escapeHtml(after)}`;
}

function escapeHtml(str) {
    return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function selectItem(index) {
    const input = document.getElementById("command-input");
    if (!input || index < 0 || index >= currentResults.length) return;

    const filepath = currentResults[index];
    const text = input.value;
    const cursor = input.selectionStart;

    // Replace @query with the filepath
    const before = text.slice(0, mentionStart);
    const after = text.slice(cursor);
    input.value = before + filepath + " " + after;

    // Position cursor after the inserted path + space
    const newPos = mentionStart + filepath.length + 1;
    input.selectionStart = input.selectionEnd = newPos;
    input.focus();

    hideDropdown();
}

function onKeydown(e) {
    if (!dropdown || dropdown.style.display === "none") return;

    if (e.key === "ArrowDown") {
        e.preventDefault();
        selectedIndex = Math.min(selectedIndex + 1, currentResults.length - 1);
        updateSelection();
    } else if (e.key === "ArrowUp") {
        e.preventDefault();
        selectedIndex = Math.max(selectedIndex - 1, 0);
        updateSelection();
    } else if (e.key === "Tab" || e.key === "Enter") {
        if (currentResults.length > 0) {
            e.preventDefault();
            e.stopImmediatePropagation();
            selectItem(selectedIndex);
        }
    } else if (e.key === "Escape") {
        e.preventDefault();
        e.stopImmediatePropagation();
        hideDropdown();
    }
}

function updateSelection() {
    if (!dropdown) return;
    dropdown.querySelectorAll(".file-mention-item").forEach((el, i) => {
        el.classList.toggle("selected", i === selectedIndex);
    });
    // Scroll selected item into view
    const selected = dropdown.querySelector(".file-mention-item.selected");
    if (selected) selected.scrollIntoView({ block: "nearest" });
}

function hideDropdown() {
    if (dropdown) dropdown.style.display = "none";
    currentResults = [];
    mentionStart = -1;
    clearTimeout(debounceTimer);
}
