# OffiCraft

**Craft your own AI office.** OffiCraft is an AI studio that runs on your own computer: you hire a few long-term AI members, hand them **whole** pieces of work, and use a web console to see how far they have got and to answer when they need your go-ahead. What runs is the Claude Code or Codex already on your machine, so the setup you have built on it still applies (your Claude Code skills, plugins, and MCP servers all stay exactly as they are).

[![OffiCraft introduction video](https://img.youtube.com/vi/OQgLX5bAfR0/maxresdefault.jpg)](https://youtu.be/OQgLX5bAfR0)

---

## Installing

### System requirements

- macOS (Apple Silicon), Linux (x86_64, arm64), or Windows 10 (version 2004 or later) / 11 (through WSL2)
- tmux 3.0 or later (a terminal tool that keeps programs running in the background). If it is missing, the install command installs it automatically: with Homebrew on macOS that has [Homebrew](https://brew.sh), and with `apt-get` (the package installer on Ubuntu and similar Linux distributions) on Linux and WSL2 that have `apt-get`; before installing, it warns that you may need to enter your Linux user's password. If macOS has no Homebrew, Linux has no `apt-get`, or the automatic install fails, the install command stops before touching any OffiCraft files and prints the commands to install what is missing. The install command only checks that tmux is installed, not its version.
- curl (a tool for downloading files): built into macOS and most Linux distributions
- At least one of Claude Code or Codex, already signed in

### Windows: preparing WSL2

On Windows, OffiCraft runs in Ubuntu inside WSL2. macOS and Linux users can skip this section.

1. Open PowerShell as administrator and run:

   ```powershell
   wsl --install -d Ubuntu
   ```

2. The first WSL install requires a restart. Then open "Ubuntu" from the Start menu and set the Ubuntu user name and password as prompted.

Every command after this runs in the Ubuntu window, which you open by clicking "Ubuntu" in the Start menu.

### Step 1: Install and sign in to Claude Code or Codex

Install whichever tool you use, or both.

Claude Code:

```bash
curl -fsSL https://claude.ai/install.sh | bash
```

Once it is installed, open a new terminal window, run `claude`, and finish signing in in the browser as prompted.

Codex:

```bash
curl -fsSL https://chatgpt.com/codex/install.sh | sh
```

Once it is installed, open a new terminal window, run `codex`, and choose "Sign in with ChatGPT" to sign in.

Check: run the matching command. If it shows a version number and sign-in information, the tool is installed and signed in.

```bash
claude --version && claude auth status --text
```

```bash
codex --version && codex login status
```

### Step 2: Install OffiCraft

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
```

The installer first checks the system requirements and automatically installs tmux if it is missing. If anything is still missing, it stops and lists the tools you need to install.

### Step 3: Open the setup link

When the install finishes, the terminal prints the following message and setup link:

```
[install] ✅ OffiCraft is installed and running in the background (…)

  open:    http://127.0.0.1:7755/?code=…
```

Open the link in a browser and set a sign-in password to enter the console. Windows users open it in a Windows browser.

### Confirm the installation

Once you sign in to the console with the password you set, the "Office" page shows the Assistant, Mira, online and able to answer chat messages. That means OffiCraft is installed.

### Updating

- If Claude Code or Codex is already installed and signed in, skip Step 1.
- OffiCraft updates are done in the console under "Settings › System update & backup"; you do not need to run the install command again.

### Uninstalling

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash -s -- --uninstall
```

Uninstalling moves the programs, database, and background service configuration to `~/.officraft.bak-<time>`; no data is deleted.

### Troubleshooting

**`command not found: claude` or `command not found: codex` appears**

1. Open a new terminal window and run the command again.
2. If it still appears, run the following command (macOS; on Linux and WSL, change `~/.zshrc` to `~/.bashrc`), then open a new terminal window:

   ```bash
   echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
   ```

**In WSL, the installer says systemd is not enabled**

1. In the Ubuntu window, run:

   ```bash
   printf '[boot]\nsystemd=true\n' | sudo tee -a /etc/wsl.conf
   ```

2. In PowerShell, run:

   ```powershell
   wsl --shutdown
   ```

3. Reopen the Ubuntu window and run the install command from Step 2 again.

**Several Ubuntu entries in the Start menu**

1. Run the following command in PowerShell to see the name of each Ubuntu:

   ```powershell
   wsl -l -v
   ```

2. Run the following command with each name in turn; the one that shows `.officraft` is the Ubuntu OffiCraft is installed in:

   ```powershell
   wsl -d <name> --cd ~ -- ls -d .officraft
   ```

3. From then on, open that Ubuntu with the following command:

   ```powershell
   wsl -d <name>
   ```

**The installer says port 7755 is already in use**

Close the program using port 7755, then run the following command to reinstall (the program is already installed at this point, so `--force` is needed):

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash -s -- --force
```

---

## Connecting from somewhere other than this computer

The server only listens on `127.0.0.1`, and this cannot be changed. To connect from a phone or another computer, tell Mira "I want to reach the console from somewhere other than this computer". See [Use it on your phone](docs/guide/mobile.md).

---

## User guide

The full user guide is in **[docs/guide/](docs/guide/)**, and the console's "Settings › User guide" reads the same files. Start with the group that matches what you want to do now.

### Getting started

Read these when you are new to OffiCraft, getting ready to install, or have just installed it and want to try it out.

- [Why OffiCraft](docs/guide/why.md) — decide whether OffiCraft suits you.
- [Install, upgrade, and uninstall](docs/guide/install.md) — prerequisites, installing, upgrading, uninstalling, and adding or removing other machines.
- [Your first office](docs/guide/quickstart.md) — in ten minutes, set your password, hand off your first piece of work, and answer your first reply card.
- [Protect your studio](docs/guide/security.md) — the password, two-factor authentication, and passkeys to set up before connecting from your phone or from outside.

### Everyday use

Read these when assigning work, answering reply cards, and checking progress and usage day to day.

- [Assigning work and answering reply cards](docs/guide/daily.md) — chat, attachments, @-mentioning members, grouping members, answering reply cards, and commenting on documents members deliver.
- [Tracking task progress](docs/guide/tasks.md) — understand task statuses, steps, and gates; accept results or force close.
- [Manage AI accounts and usage](docs/guide/usage.md) — see each account's usage, estimated cost, and the account and model each member uses in the Monitor tab; sign machines in to accounts and upgrade Claude Code.
- [Use it on your phone](docs/guide/mobile.md) — connect to the console from your phone, add it to the Home Screen, and turn on push notifications.

### Letting the studio run itself

Read these when you want to hand off repetitive, scheduled, or externally triggered work, or add members.

- [Having AI follow an SOP for repeated work (Task manuals)](docs/guide/sop.md) — write task manuals and decide which steps stop and wait for you.
- [Doing something on a schedule (Scheduled messages)](docs/guide/schedule.md) — set up scheduled messages so a member receives prewritten instructions at fixed times (the member must be online to act on them).
- [Send outside events to members automatically (webhooks)](docs/guide/webhook.md) — create webhook endpoints that hand notifications from other services (for example, new Slack messages) to the responsible member.
- [Making members remember what you taught them (Lore)](docs/guide/lore.md) — write down who you are, what must not be touched (Owner additions), each role's duties, and the lessons members have learned (lore).
- [Give members your account keys (secrets)](docs/guide/secrets.md) — store secrets so members can fetch them without pasting them into chat.
- [Adding members and hiring outsource](docs/guide/members.md) — add staff members, hire outsource members, wake and stop members, change models and machines, read the status icons, and decide between outsourcing and adding a long-term member.

### Lookup and reference

Read these to look up a field, parameter, or term, or to learn how things work underneath.

- [Interface reference](docs/guide/interface.md) — every tab and field.
- [Settings and parameters](docs/guide/settings.md) — the six settings pages and the range of each parameter.
- [Themes (look and wording)](docs/guide/theme.md) — change the look, switch by time of day, background animation, custom and imported/exported themes, seasonal themes.
- [Architecture and how it works](docs/guide/architecture.md) — how the server, machines, and members divide the work.
- [Glossary](docs/guide/glossary.md) — the terms OffiCraft uses.

### When something goes wrong

Read this when you run into problems installing or using OffiCraft, or want to back up or restore the database yourself.

- [Troubleshooting and FAQ](docs/guide/troubleshooting.md) — failed installs, members that will not start, webhook endpoints that receive no events, upgrades that did not take effect, and backup and restore.
