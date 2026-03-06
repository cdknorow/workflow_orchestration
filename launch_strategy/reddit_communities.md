# Reddit Launch Posts

## Target: r/ClaudeAI

**Title:** `I got tired of watching Claude Code scroll endlessly in my terminal, so I built a control plane for multiple AI coding agents.`

**Body:**

I've been struggling with keeping track of what my local Claude sessions are doing. This whole transition to AI coding agents has been incredible but also anxiety-inducing. I constantly feel like I'm not getting enough out of my agents, but also have a hard time managing them in so many parallel directions. 

I built **Corral** as mostly an experiment to help me better understand how the agents work, but I've been enjoying the tool enough that I thought others might also get some benefit from it. It has helped me keep track of context across sessions, and that's really started to calm my nerves.

You can install and get started with:

```bash
pip install agent-corral
cd <root-of-worktree>
launch-corral
```

What is Corral? Right now it’s an orchestration layer to track AI agents' progress all inside a single control plane. Instead of juggling different terminals, it provides a unified local web dashboard where you can:

*   See live terminal output and status updates (it integrates directly with Claude Code's `settings.json` hooks).
*   Search your entire past session history across all agents (FTS5 full-text search).
*   Attach, kill, restart, or resume wandering agents directly from the UI.
*   Tag and annotate sessions with markdown notes as they happen.
*   View auto-summarizations of past sessions to quickly regain context.

The backend is built on tmux, FastAPI, and SQLite. It even actively polls your git remote and branch state so you always know exactly what branch the agent was working on.

You can check out the source code and GIF demos here: [https://github.com/cdknorow/corral](https://github.com/cdknorow/corral)

I have lots of plans for new features. Let me know if you enjoy it or have any feedback!

---

## Target: r/commandline, r/tmux, r/LocalLLaMA

**Title:** `Corral: An open-source tmux orchestrator for running parallel AI coding agents`

**Body:**

Hi everyone,

I wanted to share an open-source tool I just released called **Corral**. It’s a wrapper and control plane designed specifically for orchestrating local AI coding agents (like Claude Code and Gemini CLI) using `tmux` under the hood. 

The problem I was trying to solve was terminal sprawl—spawning multiple AI agents in different directories and losing track of what they were doing or what state they left my git repo in.

**How it works under the hood:**
- It discovers your git worktrees and spawns a detached `tmux` session for each agent.
- It uses `tmux pipe-pane` and async file reading to stream stdout to the web dashboard without breaking the interactive terminal experience.
- A FastAPI backend analyzes the logs or listens to agent hooks (like Claude Code's `settings.json`) to extract live status pulses (e.g., `||PULSE:STATUS Running tests||`).
- It serves a local dashboard to monitor, search history (SQLite FTS5), tag sessions, and interact with the agents.

You can still just run `tmux attach -t claude-agent-1` if you want the raw terminal interface, but the web UI provides a much saner way to manage them in bulk, view auto-generated session summaries, and quickly full-text search everything an agent has ever typed.

Repo is here: [https://github.com/cdknorow/corral](https://github.com/cdknorow/corral) (`pip install agent-corral`)

Let me know what you think of the architecture!


[CLAUDE]

I've been struggling with keeping track of what my local Claude sessions are doing. This whole transition has been incredible but also, anxiety-inducing. I constantly feel like I'm not getting enough out of my agents, but also having a hard time managing them in so many parallel directions. 

I built **Corral** as mostly an experiment to help me better understand how the agents work, but I've been enjoying the tool enough that I thought others might also get some benefit from it. Its helped to keep track of my context across sessions and thats really started to calm my nerves. 

You can install and get started with

```
pip install agent-coral
cd <root-of-worktree>
corral
```

What is Corral? Right now it’s an orchestration layer to tracks AI agents progress all inside a single control plane. Instead of juggling different terminals, it provides a unified web dashboard where you can:

*   See live terminal and status updates.
*   Search your entire past session history across all agents (FTS5 search).
*   Attach, kill, or restart wandering agents directly from the UI.
*   Tag and annotate sessions as they happen.


The backend is built on tmux, FastAPI and SQLite, and it actively polls your git state to keep track of what branches the agents are operating on.

You can check out the source code and the GIF demos here: [https://github.com/cdknorow/corral](https://github.com/cdknorow/corral)

I have lots of plans for new features, let me know if you enjoy or have any feedback.