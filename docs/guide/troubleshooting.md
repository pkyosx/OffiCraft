# Troubleshooting and FAQ

This page lists causes and where to fix them, organized by the symptom you see on screen. It covers installing, signing in, members that will not start, webhook endpoints that receive no events, members that do nothing or will not stop, backup and restore, upgrading, and removal. Each entry gives, in order, what you see, what causes it, and where to fix it.

## Installing

### The install script says this computer is not supported

OffiCraft supports three kinds of computer: Apple Silicon Macs (darwin/arm64), x86_64 Linux (amd64), and arm64 Linux (aarch64). Intel Macs and Linux on other processor architectures are refused at the very start of the install, before anything is downloaded.

Running directly on Windows is not supported either. On Windows, first install WSL2 and Ubuntu, then install inside Ubuntu. For the steps, see "Windows (WSL2)" in [Install, upgrade, and uninstall](install.md).

### The install script says systemd is not running

On Linux, OffiCraft runs in the background as systemd user services (systemd is the Linux system component that starts and stops background services). When systemd is not running, the install script stops without installing anything.

This happens most often on Windows WSL2, because WSL2 does not start systemd by default. For how to turn systemd on, see "Windows (WSL2)" in [Install, upgrade, and uninstall](install.md): add `[boot]` and `systemd=true` to `/etc/wsl.conf`, run `wsl.exe --shutdown` from Windows, reopen the Ubuntu window, and run the install again. WSL versions earlier than 0.67.6 do not support systemd; update first with `wsl --update`.

Running `ps -p 1 -o comm=` in the Ubuntu window prints `systemd` when systemd is running.

### The install script says tmux or an agent runtime is missing

Before touching the machine, the install script checks for `tmux` and for at least one of `claude` (Claude Code) or `codex` (Codex). If `tmux` is missing, it installs it automatically first (Homebrew on macOS, `apt-get` on Linux and WSL2). If macOS has no Homebrew, Linux has no `apt-get`, the automatic install fails, or both Claude Code and Codex are missing, the script stops before touching any OffiCraft files and prints the commands to install what is missing in the terminal; by then `tmux` may already have been installed automatically. How to install Claude Code and Codex is in "Step 1: Install and sign in to Claude Code or Codex" in [Install, upgrade, and uninstall](install.md). Once it is installed, run the install command again. Installing only one of the two is fine, but the one you install must already be signed in for members to start. On Windows (WSL2), Claude Code or Codex must be installed inside WSL2's Ubuntu; a copy installed on the Windows side does not count.

### Port 7755 is in use

By default the server uses port 7755 (a port is the number a computer uses to tell different network services apart). If another program is using port 7755, the install script lists that program and stops. At that point the program is installed and the database is ready; only the server has not started. Either close the program using the port, or have the server use a different port, then run the install again.

The server's port is set in the `[server]` section of `oc.toml`, for example:

```toml
[server]
port = 7756
```

The install script reads `[server].port` only from the file named by `$OC_CONFIG` or from `oc.toml` in the folder you run the install from; it reads no other environment variables. The server reads the same `oc.toml` every time it starts afterwards, so **if that file is moved or deleted, the server can no longer see the changed port**. If you change the port while the server is already running, the install script stops and lists the differences; it only applies the change when you run it again with `--relocate`.

After changing the port, the console address becomes `http://localhost:<new port>`. For other install options, see "Advanced: install options" in [Install, upgrade, and uninstall](install.md).

### Lost the claim code for setting the admin password

The claim code works only once and stops working after you set a password.

- If you have already set a password: signing in only needs the password, not the claim code.
- If you have not set a password yet: running `~/.officraft/bin/ocserverd claim-token` on the server machine prints the current claim code. After a password is set, `claim-token` no longer prints a claim code.

### "Automatic setup did not finish" appears at the top of the console

After you set your password, the server automatically installs this machine's warden and wakes the Assistant, Mira. If one of those steps fails, an "Automatic setup did not finish" banner appears at the top of the console, naming the step and the reason, with "Show details".

- "Install this machine" failed: the reason is in the details. Once the cause is fixed, "Install" in this machine's name menu under Monitor › Machines redoes this step.
- Automatic setup was interrupted (for example, the server happened to restart): "Install" under Monitor › Machines finishes installing the machine; Mira has to be woken separately.
- It says "The initial setup did not finish": restarting the server runs the initial setup again.

## Signing in

### Forgot your password, lost your authenticator, or a passkey stops working

All three are handled with the recovery commands of `~/.officraft/bin/ocserverd` on the machine the server is installed on. Being able to run commands on that machine proves you are the owner of the studio, so no other verification is needed. The command to restart the server:

```bash
# macOS
launchctl kickstart -k gui/$(id -u)/com.officraft.serve
# Linux and Windows (WSL2)
systemctl --user restart com.officraft.serve.service
```

- **Reset your password**: `OC_NEW_PASSWORD='new password' ~/.officraft/bin/ocserverd set-password`. The new password is passed in through the `OC_NEW_PASSWORD` environment variable; the command itself takes no arguments. The new password does not take effect until the server restarts.
- **Turn off two-factor authentication**: `~/.officraft/bin/ocserverd mfa-disable`. Until the server restarts, signing in still asks for a code.
- **Delete all passkeys**: `~/.officraft/bin/ocserverd passkey-reset`. All passkeys are deleted immediately, without restarting the server; afterwards, sign in with your password.

What to keep in mind about two-factor authentication and passkeys is in [Protect your studio](security.md).

## A member will not start

### Where to look first

The reason a member will not start is recorded in three places:

- **"Last operation" in Member details**: the result and reason of the last wake. This is the most accurate line.
- **Monitor › Machines**: whether the member's machine is online, and whether the Claude and Codex columns show a version, "not installed", "signed out", or "too old".
- **The warden log on that machine**: `~/.officraft/warden/log/ocwarden.err.log` records in detail how the warden started the member. The location is the same on macOS and Linux.

After the server sends a wake, the member first shows "Waking". If it does not connect within about two minutes, the wake counts as failed and the reason is written to Last operation.

### "No wake command went out this time" pops up

This means the server did not send out the wake command this time; it retries automatically in the background. There are two common reasons:

- The target machine is not connected. Whether a machine is online is shown in Monitor.
- An earlier wake is still being retried. The retries are recorded in this member's "Last operation".

If the time on Last operation is earlier than when you pressed the button this time, it shows the result of the previous wake, which may not apply to this one.

### Last operation says "signed out of Claude" or "signed out of Codex"

The member's machine is not signed in to the AI runtime the member needs. You sign in from "Sign in" in the version-number menu of that machine's Claude or Codex column, under Monitor › Machines. After signing in, the member has to be woken again before it starts.

The warden checks the sign-in status regularly, at the intervals set by "Login check interval" and "Logged-out recheck interval" in Settings › General.

### The machine has only Codex, and Mira or a new member will not start

When a member starts on a machine, it decides between Claude Code and Codex based on the capabilities that machine reports. In the following cases the member starts with Claude Code, but that machine has no Claude Code:

- `codex` is installed but not signed in, so it does not count as available. This start does not save Claude Code as the member's setting; sign in to Codex and wake the member again, and it switches to Codex.
- That machine's warden has not reported its capabilities yet. Again nothing is saved; once the warden reports and you wake the member again, it uses Codex.
- The member's runtime has already been saved as Claude Code: someone chose Claude Code in the member's settings, or the member was carried over from an older version.

For the third case, sign in to Codex under Monitor › Machines and change "AI runtime" to Codex in Member details (while the member is online or waking, this setting is behind the "Change" button; while it is not online, it is behind the "Wake" button). If the member is currently waking, the new runtime only takes effect at its next start. Members added to this machine afterwards use Codex automatically; **members already saved as Claude Code do not switch automatically**, and each one has to be changed separately.

### Last operation says Codex has no model in some family

The Codex model family set for the member cannot be found in that machine's Codex version, or the OffiCraft program on that machine is too old to recognize the model family. Either update Codex or OffiCraft on that machine, or change the member in Member details to a model that machine can use.

### Last operation says notify_claude_too_old

The Claude Code version on that machine is older than 2.1.317, the oldest that both loads the OffiCraft notification module and records which account each session uses, so the warden did not start the member. A machine whose version is too old is marked "too old" in the Claude column under Monitor › Machines, and "Upgrade Claude Code" in that column's version-number menu upgrades it. After upgrading, the member has to be woken again.

### Last operation says notify_mod_not_loaded or wake_timeout

Claude Code started, but the OffiCraft notification module did not load in time, so the member did not connect to the server. The warden first restarts Claude Code once automatically; if it still fails, the message also says "Claude Code was restarted once automatically and the module still did not load", followed by the switch value read before each launch. After a while, Last operation switches to the less specific `wake_timeout`, and the reason is kept only in the warden log.

The actual cause is recorded on that machine: lines containing `notify-mod` in the warden log `~/.officraft/warden/log/ocwarden.err.log`, and two marker files in the member's folder `~/.officraft/agents/<member ID>/`: `.officraft-mod-started` (created when the module starts running) and `.officraft-mod-loaded` (created when the module starts the connection program). What each record means:

- The log has `.officraft-mod-started written ...s after launch`: the module ran, but Claude Code was slow to start. The lines starting with `pane|` in the log are the last few lines of the member's screen at the time, showing which dialog or error it stopped at.
- The log has `.officraft-mod-started absent`: the module did not run at all. The most common cause is that the organization's plugins synced when Claude Code started had changed, and the screen shows `Plugins changed. Run /reload-plugins to activate.`; the warden sends `/reload-plugins` once automatically. If it still fails, other possible causes are that the working directory is not trusted by Claude Code, or that Claude Code settings have turned hooks or plugins off (for example `disableAllHooks`, `--safe-mode`, or settings managed centrally by your company).
- `.officraft-mod-loaded` exists but the member is still not online: the module did start the connection program, but the connection program later exited. `pgrep -fl 'ocagent listen'` shows whether it is still running.

### Claude Code or Codex was installed with a version manager, and members cannot find it

A `claude` or `codex` installed with tools such as asdf, nvm, or volta may not be found by background services, because background services only look for programs in a few fixed folders (the PATH, the list of folders the system searches for programs). The install script records the path it found at install time. If it found none, run the install again on the server machine, specifying the location with `OC_CLAUDE_BIN=/absolute/path/claude` or `OC_CODEX_BIN=/absolute/path/codex`, for example:

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | OC_CLAUDE_BIN=/absolute/path/claude bash
```

Running the install again does not affect existing data.

### No member on a whole machine will start

Check that machine's status under Monitor › Machines:

- **Offline**: the machine is shut down or cannot reach the server. Each machine's warden runs independently, so one offline machine only affects the members on it.
- **Offline for a long time, and still not back after reconnecting to the network**: the machine credential may have expired. Each machine's credential lasts for "Machine credential lifetime" in Settings › General (30 days by default), and the warden renews it automatically before it expires. If the machine was shut down long enough to miss the renewal, reinstall that machine. "Install" in the machine's name menu ("Reinstall" while it is online) produces the install command to run on that machine.
- **The machine column has an exclamation mark saying "Not in effect"**: stop every member on this machine, then wake them again one by one, and it recovers.

### Nobody ever takes the outsource work

- The "Outsource" tab in the Office sidebar shows "Assignment paused": the outsource limit is 0. The limit is "Max hires" under "Recruit a member" in the "Outsource" tab.
- The task manual's assignee is outsource, but no machine is selected, or the selected machine is offline: outsource members only start on the selected machine. The machine is selected in Settings › Task manuals, and outsource members start once the selected machine is online.

### On Windows (WSL2), do members go offline when I close the terminal?

No. Windows only keeps WSL2's Ubuntu running while there is still a Linux program started from the Windows side; Ubuntu windows count, systemd user services do not. So when installing on WSL2, OffiCraft puts a keep-alive file in the Windows "Startup" folder and runs it once right away. The keep-alive file starts, in the background, a Linux program that does nothing and never exits (`sleep infinity`), without adding any window on screen. With `sleep infinity` running, Ubuntu and the warden and members inside it keep running after every Ubuntu window is closed. Each time you sign in to Windows, the keep-alive file in the "Startup" folder runs again.

- **The keep-alive file is named `OffiCraft keep WSL running (<name>).vbs`.** `<name>` is the name of the Linux distribution OffiCraft is installed in, for example `Ubuntu`; running `wsl -l -v` in PowerShell lists the names. A second studio's warden (installed with `OC_NAMESPACE`) has its own keep-alive file named `OffiCraft keep WSL running (<name>, <studio name>).vbs`. Pressing the Windows key with R, typing `shell:startup`, and pressing Enter opens the "Startup" folder. The keep-alive file contains only one line; when the name is `Ubuntu`, it is:

  ```vb
  CreateObject("WScript.Shell").Run "wsl.exe -d Ubuntu --exec sleep infinity", 0, False
  ```

- **If the install prints `WARN: WSL keep-alive not set up`, the keep-alive file was not set up.** A common cause is that the Linux distribution OffiCraft is installed in has Windows interop turned off (the ability to run Windows programs from Linux, set by `[interop]` in `/etc/wsl.conf`). In that case, once every Ubuntu window is closed, Windows stops the whole Ubuntu, and this machine shows as offline in "Machines". Reopening an Ubuntu window needs nothing else: the warden starts automatically with Ubuntu, and the members that were on this machine come back online in about half a minute. To keep members working after the windows are closed, run the command printed on the WARN line in Windows PowerShell; when the name is `Ubuntu`, it is:

  ```powershell
  Start-Process wsl.exe -ArgumentList '-d','Ubuntu','--exec','sleep','infinity' -WindowStyle Hidden
  ```

  The `sleep infinity` started by the PowerShell command only lasts until you sign out of Windows or restart. To start it automatically every time you sign in to Windows, use Notepad to create a file containing the `CreateObject(…)` line above, save it with a `.vbs` extension, and put it in the "Startup" folder.

- **How to stop it**: run `wsl --terminate <name>` in PowerShell, and Ubuntu stops along with OffiCraft and the members inside it. To stop it from running automatically when you sign in to Windows, delete the keep-alive file from the "Startup" folder. Removing the warden with `ocwarden teardown` also deletes the keep-alive file; a `sleep infinity` that is already running lasts until you sign out of Windows or restart.
- **Members still go offline when the computer sleeps.** For members to keep working, this computer has to stay awake.

## Webhook endpoints

### The outside service sent an event, but the member did not receive it

The outside service showing success does not mean the member received it: whether the request was delivered, dropped, or sent to a token that does not exist, the outside service gets the same response (except when the request body is larger than 1 MiB, in which case the outside service gets a 413 error; Slack's verification handshake also gets a different response). The reason for a drop is recorded in "Event stats" on that endpoint's row in the "WEBHOOK ENDPOINTS" card in Member details:

- **signature failed**: the Signing Secret on the Slack or GitHub side does not match the one here. Setting both sides to the same secret fixes it.
- **hit while disabled**: the endpoint was disabled, and the event was dropped when it arrived. Turning the "Enabled" switch on fixes it.
- **member gone**: the member the endpoint belonged to has left; recreate the endpoint on another member.
- **oversize**: the request body was larger than 1 MiB, and the system rejected it entirely.

If the member is offline or stopped, delivered events wait until the member next comes online. For a full description of each reason, see "Webhook endpoints" in [Interface reference](interface.md).

## A member is online but doing nothing

The member's dot shows "Online", but no reply or progress comes. Possible causes:

- **An exclamation mark next to the name**: the member cannot use its AI model normally right now, and the exclamation mark gives the reason:
  - **usage limit reached**: the account's usage has run out. The message says at what time today or tomorrow it resets, and the member continues after the reset. Usage is shown under Monitor › Accounts.
  - **sign-in expired**, **signed out of Claude / signed out of Codex**: that machine's sign-in is no longer valid. Signing in to Claude or Codex again under Monitor › Machines fixes it.
  - **server error**: an error on the model service's side, which usually clears up on its own after a while.
  - **model call failed (with a code)**: the details of the failure are in "Last operation" in Member details, and the code indicates the kind of failure.
- **Activity status icon**: "Working" means the member is doing something (possibly through a sub-agent, an AI worker the member starts to handle part of its work); "Waiting" means it is waiting for a command running in the background or a schedule; "Idle" means the member is not doing anything. Details are shown in "Runtime".
- **A reply card is waiting for you**: when a member hits something you need to decide, it opens a reply card and stops, continuing only after you reply; the Assistant can also reply for you. While other reply cards on the same task are still unanswered, the task stays at "Awaiting my reply". Reply cards are listed in the Ask tab.
- **The task's status**:
  - **Waiting on external**: waiting for a permission, someone else's reply, or a certain time. It is not a problem with the member itself.
  - **Ready to close**: every step is done, and the task is waiting for its owner to close it. If the owner does not finish up, the menu on the task card's "Ready to close" label has "Force close"; **force closing cannot be undone**.
  - **Frozen**: a task whose priority is Frozen stops moving forward.

  For what each task status means, see [Tracking task progress](tasks.md).
- **High memory use**: when a member's memory is nearly full, its replies get worse. "Refocus" in Member details has an online member write its progress into a handover and continue from that handover in a new working session. See [Adding members and hiring outsource](members.md).

## A member will not stop

### A member stays at "Stopping" for a long time

A normal stop has no time limit; the server keeps waiting for the member to finish wrapping up on its own. "Accelerated stop" in Member details gives the wrap-up a deadline (120 seconds by default); "Force stop" ends the member immediately, and **unsaved work is lost**.

## Backup and restore

### Where to see backup health

The studio backs up its database automatically every 6 hours. The backup status is in "Backup health" under Settings › System update & backup:

| Status | Meaning |
| --- | --- |
| Backups are healthy | The most recent scheduled backup is within its deadline. |
| Backups are failing | There has never been a backup, the latest backup is too old (the schedule may have stopped), or the most recent backup failed. It says how long backups have been failing. |
| Cannot tell | Not evaluated yet, or the console cannot reach the server. This does not mean everything is fine; treat it as needing a check. |

Only a scheduled backup returns the status to healthy. Running `ocserverd backup` by hand or the automatic backup before an upgrade does not count, and restarting the server does not clear the failing status either. **Backup health does not notify you**; failing backups are shown only under Settings › System update & backup.

### Backing up the database yourself

The database is in `~/.officraft/server/data/` and consists of three files: `officraft.db`, `officraft.db-wal`, and `officraft.db-shm`. **Copying only `officraft.db` while the server is running loses data**: recently written data is in the `-wal` file, so a backup of only the main file looks fine but is missing the latest data after a restore.

There are two ways to make a complete backup:

- **Without stopping**: `~/.officraft/bin/ocserverd backup` produces a single file containing all the data in `~/.officraft/server/data/backups/`. Before running, it prints the config file and database path it uses; `database` is the database actually being backed up.
- **Copy after stopping**: after the server stops, copy all three files together, or copy the whole `data` folder.

Automatic backups go into the same `backups/` folder, named `officraft-<UTC time>-<kind>.db`, where the kind is `scheduled`, `premigration` (before an upgrade), or `manual`. Routine backups and pre-upgrade backups each keep "Backups kept" copies (Settings › General; default 5, range 1–20), and any beyond that are deleted at the next backup.

### Take vault.key with you when you change computers

Database backups (automatic or manual) never include `vault.key`, and the secret values and the not-yet-deleted content of Secret reply cards are encrypted with `vault.key`. When restoring on the same computer, `vault.key` is still in its original folder and needs no attention. When moving a backup to another computer to restore it, take `vault.key` along; otherwise the secrets cannot be read after the restore. For details, see "Secrets and vault.key" in [Protect your studio](security.md).

### Restoring from a backup

Restoring means replacing the database in `~/.officraft/server/data/` with a backup file while the server is stopped: move the existing `officraft.db`, `officraft.db-wal`, and `officraft.db-shm` somewhere else, then copy the backup file to `officraft.db` (for example `cp backups/officraft-20261020-060000-scheduled.db officraft.db`). The same folder must contain the `vault.key` from that time; without it, the secrets cannot be read after the restore.

Commands to stop and start the server on macOS:

```bash
launchctl bootout gui/$(id -u)/com.officraft.serve
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.officraft.serve.plist
```

On Linux and Windows (WSL2):

```bash
systemctl --user stop com.officraft.serve.service
systemctl --user start com.officraft.serve.service
```

## Upgrading

### An upgrade did not take effect, or checking for updates failed

"The update check failed" usually means GitHub cannot be reached (OffiCraft updates are published on GitHub; network or proxy problems both cause it), and checking for updates succeeds once the network is back. Wardens on other machines update themselves after the server upgrades, usually within 15 minutes. For the full upgrade steps and each update setting, see "Upgrading" and "Advanced: upgrade settings" in [Install, upgrade, and uninstall](install.md).

## Removal

For the steps to remove one machine (uninstall, delete) or the whole studio, see "Uninstalling" and "Removing a machine from the console" in [Install, upgrade, and uninstall](install.md).

## Related pages

- [Install, upgrade, and uninstall](install.md)
- [Protect your studio](security.md)
- [Adding members and hiring outsource](members.md)
- [Tracking task progress](tasks.md)
- [Architecture and how it works](architecture.md)
