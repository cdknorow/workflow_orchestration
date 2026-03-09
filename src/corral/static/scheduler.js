/* Scheduler: scheduled jobs sidebar list, job detail view, and CRUD operations */

import { state } from './state.js';
import { showToast } from './utils.js';

let scheduledJobs = [];
let selectedJobId = null;

// ── API helpers ──────────────────────────────────────────────────────────

async function fetchJobs() {
    try {
        const resp = await fetch('/api/scheduled/jobs');
        const data = await resp.json();
        scheduledJobs = data.jobs || [];
        renderJobsSidebar();
    } catch (e) {
        console.error('Failed to fetch scheduled jobs:', e);
    }
}

async function fetchJobRuns(jobId) {
    try {
        const resp = await fetch(`/api/scheduled/jobs/${jobId}/runs?limit=20`);
        const data = await resp.json();
        return data.runs || [];
    } catch (e) {
        console.error('Failed to fetch runs:', e);
        return [];
    }
}

// ── Sidebar rendering ────────────────────────────────────────────────────

function renderJobsSidebar() {
    const list = document.getElementById('scheduled-jobs-list');
    if (!list) return;
    if (!scheduledJobs.length) {
        list.innerHTML = '<li class="empty-state">No scheduled jobs</li>';
        return;
    }
    list.innerHTML = scheduledJobs.map(job => {
        const active = selectedJobId === job.id ? 'active' : '';
        const status = job.enabled ? '' : ' (paused)';
        const lastStatus = job.last_run ? job.last_run.status : '';
        const dot = lastStatus === 'completed' ? '🟢'
            : lastStatus === 'running' ? '🔵'
            : lastStatus === 'failed' || lastStatus === 'killed' ? '🔴'
            : '⚪';
        return `<li class="session-list-item ${active}" onclick="selectScheduledJob(${job.id})">
            <span class="sched-dot">${dot}</span>
            <span class="session-name">${escapeHtml(job.name)}${status}</span>
            <span class="sched-cron" style="font-size:10px;color:var(--text-muted);margin-left:auto">${escapeHtml(job.cron_expr)}</span>
        </li>`;
    }).join('');
}

function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

// ── Job detail view ──────────────────────────────────────────────────────

export async function selectScheduledJob(jobId) {
    selectedJobId = jobId;
    renderJobsSidebar();

    const job = scheduledJobs.find(j => j.id === jobId);
    if (!job) return;

    // Hide other views, show scheduler view
    document.getElementById('welcome-screen').style.display = 'none';
    document.getElementById('live-session-view').style.display = 'none';
    document.getElementById('history-session-view').style.display = 'none';
    document.getElementById('scheduler-view').style.display = 'block';

    // Deselect live/history sessions
    state.currentSession = null;
    document.querySelectorAll('.session-list-item.active').forEach(el => {
        if (!el.closest('#scheduled-jobs-list')) el.classList.remove('active');
    });

    const runs = await fetchJobRuns(jobId);
    renderJobDetail(job, runs);
}

function renderJobDetail(job, runs) {
    const container = document.getElementById('scheduler-detail');
    if (!container) return;

    const enabledLabel = job.enabled ? 'Enabled' : 'Paused';
    const toggleLabel = job.enabled ? 'Pause' : 'Enable';
    const nextFire = job.next_fire_at ? new Date(job.next_fire_at).toLocaleString() : 'N/A';

    container.innerHTML = `
        <div class="sched-header">
            <h2>${escapeHtml(job.name)}</h2>
            <div class="sched-actions">
                <button class="btn btn-small" onclick="toggleScheduledJob(${job.id})">${toggleLabel}</button>
                <button class="btn btn-small btn-warning" onclick="deleteScheduledJob(${job.id})">Delete</button>
            </div>
        </div>
        ${job.description ? `<p class="sched-desc">${escapeHtml(job.description)}</p>` : ''}
        <dl class="info-grid" style="margin-bottom:16px">
            <dt>Schedule</dt><dd><code>${escapeHtml(job.cron_expr)}</code> (${escapeHtml(job.timezone)})</dd>
            <dt>Status</dt><dd>${enabledLabel}</dd>
            <dt>Next Run</dt><dd>${nextFire}</dd>
            <dt>Agent</dt><dd>${escapeHtml(job.agent_type)}</dd>
            <dt>Repo</dt><dd style="word-break:break-all">${escapeHtml(job.repo_path)}</dd>
            <dt>Branch</dt><dd>${escapeHtml(job.base_branch || 'main')}</dd>
            <dt>Timeout</dt><dd>${job.max_duration_s}s</dd>
            <dt>Cleanup Worktree</dt><dd>${job.cleanup_worktree ? 'Yes' : 'No'}</dd>
            ${job.flags ? `<dt>Flags</dt><dd><code>${escapeHtml(job.flags)}</code></dd>` : ''}
        </dl>
        <div class="sched-prompt-section">
            <h3>Prompt</h3>
            <pre class="sched-prompt">${escapeHtml(job.prompt)}</pre>
        </div>
        <div class="sched-runs-section">
            <h3>Run History</h3>
            ${runs.length ? renderRunsTable(runs) : '<p class="empty-state">No runs yet</p>'}
        </div>
    `;
}

function renderRunsTable(runs) {
    return `<table class="sched-runs-table">
        <thead><tr><th>Scheduled</th><th>Status</th><th>Duration</th><th>Exit</th></tr></thead>
        <tbody>${runs.map(r => {
            const scheduled = new Date(r.scheduled_at).toLocaleString();
            const duration = r.started_at && r.finished_at
                ? formatDuration(new Date(r.finished_at) - new Date(r.started_at))
                : '-';
            const statusClass = r.status === 'completed' ? 'status-ok'
                : r.status === 'failed' || r.status === 'killed' ? 'status-err'
                : r.status === 'running' ? 'status-running'
                : '';
            const sessionLink = r.session_id
                ? `<a href="#session/${r.session_id}" class="run-session-link" title="View session">${r.session_id.substring(0, 8)}</a>`
                : '';
            return `<tr>
                <td>${scheduled}</td>
                <td><span class="sched-status ${statusClass}">${r.status}</span> ${sessionLink}</td>
                <td>${duration}</td>
                <td>${escapeHtml(r.exit_reason || '-')}</td>
            </tr>`;
        }).join('')}</tbody>
    </table>`;
}

function formatDuration(ms) {
    const s = Math.floor(ms / 1000);
    if (s < 60) return `${s}s`;
    const m = Math.floor(s / 60);
    if (m < 60) return `${m}m ${s % 60}s`;
    const h = Math.floor(m / 60);
    return `${h}h ${m % 60}m`;
}

// ── CRUD operations ──────────────────────────────────────────────────────

export async function toggleScheduledJob(jobId) {
    try {
        await fetch(`/api/scheduled/jobs/${jobId}/toggle`, { method: 'POST' });
        await fetchJobs();
        if (selectedJobId === jobId) selectScheduledJob(jobId);
    } catch (e) {
        showToast('Failed to toggle job', true);
    }
}

export async function deleteScheduledJob(jobId) {
    if (!confirm('Delete this scheduled job and all its run history?')) return;
    try {
        await fetch(`/api/scheduled/jobs/${jobId}`, { method: 'DELETE' });
        showToast('Job deleted');
        selectedJobId = null;
        document.getElementById('scheduler-view').style.display = 'none';
        document.getElementById('welcome-screen').style.display = '';
        await fetchJobs();
    } catch (e) {
        showToast('Failed to delete job', true);
    }
}

// ── Job creation modal ───────────────────────────────────────────────────

export function showJobModal() {
    document.getElementById('job-modal').style.display = 'flex';
    document.getElementById('job-modal-name').value = '';
    document.getElementById('job-modal-description').value = '';
    document.getElementById('job-modal-cron').value = '0 2 * * *';
    document.getElementById('job-modal-timezone').value = 'UTC';
    document.getElementById('job-modal-repo').value = '';
    document.getElementById('job-modal-branch').value = 'main';
    document.getElementById('job-modal-agent').value = 'claude';
    document.getElementById('job-modal-prompt').value = '';
    document.getElementById('job-modal-timeout').value = '3600';
    document.getElementById('job-modal-cleanup').checked = true;
    document.getElementById('job-modal-flags').value = '';
    document.getElementById('cron-preview').innerHTML = '';
    validateCronPreview();
}

export function hideJobModal() {
    document.getElementById('job-modal').style.display = 'none';
}

export async function validateCronPreview() {
    const expr = document.getElementById('job-modal-cron').value.trim();
    const tz = document.getElementById('job-modal-timezone').value.trim() || 'UTC';
    const preview = document.getElementById('cron-preview');
    if (!expr) { preview.innerHTML = ''; return; }

    try {
        const resp = await fetch('/api/scheduled/validate-cron', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ cron_expr: expr, timezone: tz }),
        });
        const data = await resp.json();
        if (data.valid) {
            preview.innerHTML = '<span style="color:var(--green)">Valid</span> — Next: ' +
                data.next_fire_times.slice(0, 3).map(t => new Date(t).toLocaleString()).join(', ');
        } else {
            preview.innerHTML = `<span style="color:var(--red)">Invalid:</span> ${escapeHtml(data.error || 'bad expression')}`;
        }
    } catch (e) {
        preview.innerHTML = '<span style="color:var(--red)">Error validating</span>';
    }
}

export async function createScheduledJob() {
    const body = {
        name: document.getElementById('job-modal-name').value.trim(),
        description: document.getElementById('job-modal-description').value.trim(),
        cron_expr: document.getElementById('job-modal-cron').value.trim(),
        timezone: document.getElementById('job-modal-timezone').value.trim() || 'UTC',
        repo_path: document.getElementById('job-modal-repo').value.trim(),
        base_branch: document.getElementById('job-modal-branch').value.trim() || 'main',
        agent_type: document.getElementById('job-modal-agent').value,
        prompt: document.getElementById('job-modal-prompt').value.trim(),
        max_duration_s: parseInt(document.getElementById('job-modal-timeout').value) || 3600,
        cleanup_worktree: document.getElementById('job-modal-cleanup').checked,
        flags: document.getElementById('job-modal-flags').value.trim(),
    };

    if (!body.name || !body.cron_expr || !body.repo_path || !body.prompt) {
        showToast('Name, cron, repo path, and prompt are required', true);
        return;
    }

    try {
        const resp = await fetch('/api/scheduled/jobs', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        const data = await resp.json();
        if (data.error) {
            showToast(data.error, true);
            return;
        }
        showToast('Job created');
        hideJobModal();
        await fetchJobs();
        selectScheduledJob(data.id);
    } catch (e) {
        showToast('Failed to create job', true);
    }
}

// ── Init ─────────────────────────────────────────────────────────────────

export function initScheduler() {
    fetchJobs();
    // Refresh jobs every 30s
    setInterval(fetchJobs, 30000);
}
