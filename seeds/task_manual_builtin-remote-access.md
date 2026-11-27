# Background

The console only accepts local connections (`127.0.0.1`) on the computer it runs on. To reach it from a phone or another device, forwarding must be set up on that computer to relay external HTTPS connections to the console's local port.

The scope of this task is limited to:

- Installing and configuring the forwarding tool.
- Setting the forwarding service to run automatically after boot.
- Verifying that the forwarded URL opens the console.
- When necessary, moving existing studio machines to the new console URL.

This task does not include:

- Changing console settings.
- Changing router or firewall settings.
- Logging in to any account for the user.

Any operation that needs administrator privileges (for example `sudo` or the computer's password), or that requires the user to log in to an account personally, always goes through an action reply card. The card must explain the operation and the expected result; the user completes it themselves. After the reply, read back the result and continue.

You must not ask the user for passwords or other account credentials.

The following three forwarding methods are available:

- **cloudflared**: creates a fixed public HTTPS URL on the console computer. Connecting devices need no extra app. It uses the user's own domain, so the user must have a Cloudflare account and a domain managed by Cloudflare.
- **Tailscale Funnel**: uses a public `ts.net` URL from Tailscale; no domain is needed. Connecting devices do not need Tailscale; the console computer must have Tailscale installed and logged in to an account.
- **Tailscale (devices in the account only)**: only devices logged in to the same Tailscale account can connect. The console computer and every device that needs to connect must have Tailscale installed and logged in to that same account.

# SOP

## 1. Security check

Use `get_login_protection` to check whether two-factor authentication is enabled.

If it is not enabled, open a decision reply card. When opening the card, you must explain:

- Two-factor authentication is not currently enabled on the console.
- Once remote access is open, the console login page is reachable from other devices; anyone who can log in can interact with AI members and operate the computer through them, so enabling two-factor authentication first is recommended.
- A passkey cannot replace two-factor authentication; even with a passkey set up, the login page still accepts password login.

Ask the user to decide:

- Enable two-factor authentication first, then continue.
- Keep the current login protection and continue opening remote access.
- Do not open remote access.

If the user chooses to enable two-factor authentication first, ask them to set it up at "avatar at the top right of the console › Account security". After the reply, use `get_login_protection` again to confirm it is enabled before continuing.

If the user confirms keeping the current login protection and continuing, act on that decision; do not stop the task because two-factor authentication is off.

If the user chooses not to open remote access, do not set up new forwarding; record the user's decision and end the task.

If the console already has external forwarding but two-factor authentication is not enabled, the card must also explain that the console is already reachable from outside, and ask the user to decide whether to keep the current state, enable two-factor authentication, or shut down the existing forwarding.

**Completion condition**

- The current two-factor authentication status has been confirmed.
- If two-factor authentication is not enabled, the user has decided through a reply card whether to proceed with opening or keeping remote access.

## 2. Confirm the forwarding method

### 2.1 Confirm where you are running

Confirm that the current member is running on the console computer.

When your own server URL (`OC_BASE`) is `127.0.0.1` or `localhost`, treat it as running on the console computer, and take the console port from that URL.

If not, stop and open a reply card asking the user to reassign the task to a member running on the console computer.

### 2.2 Confirm the current forwarding

Check whether forwarding to the console already exists. If so, identify its method, URL, and how it is started, and confirm:

- Whether it points to the current console port.
- Whether the URL can be used persistently rather than being a temporary URL.
- Whether it is already set to run automatically after boot.
- Whether it also serves other services, and whether changing its settings could affect existing services.

This step only confirms the current state; do not reuse, modify, or shut down existing forwarding on your own. The user decides whether to reuse it in step 2.4.

The following situations need special attention:

- **cloudflared**: `trycloudflare.com` is a temporary URL and is not listed as reusable forwarding. When judging whether cloudflared serves other URLs and whether it runs automatically at boot, you must check the actual startup configuration; do not judge by whether `cloudflared service install` was ever run.
- **Tailscale**: confirm whether Funnel or forwarding limited to devices in the Tailscale account is in use, and the current access scope.

You must not shut down or overwrite existing forwarding to reconfigure. Existing forwarding may still be used by other machines or services; disable it only after the new connection method is verified and any necessary migration is done.

### 2.3 Confirm other machines

Use `list_machines` to check the studio's other machines.

If other machines connect through the current forwarding URL, changing or shutting it down may disconnect those machines and their members. Record the affected machines, and explain them when the user decides on the forwarding method in the next step.

### 2.4 Confirm the forwarding method

Open a decision reply card and, based on the state confirmed above, ask the user to decide which forwarding method to use this time.

If existing forwarding meets the need and can be used persistently, offer a "Keep the current forwarding" option, and explain its URL, access scope, and whether other machines use it.

Describe the remaining options by the user's own situation, without requiring them to understand forwarding tools in advance:

- **I already have a Cloudflare account and my own domain**: use cloudflared. On the same card, ask the user for the domain to use.
- **I have no Cloudflare account or own domain, and want to connect directly from outside (recommended)**: use Tailscale Funnel. The URL is provided by Tailscale, no domain needs to be prepared, and anyone who knows the URL can reach the login page.
- **I have no Cloudflare account or own domain, and only want my own devices to be able to connect**: use Tailscale's private forwarding. Every device that needs to connect must have Tailscale installed and be logged in to the same account.

If a method does not fit the current environment, do not list it; explain why on the reply card.

If cloudflared already serves other URLs or services, do not modify its existing configuration to add the console. If you cannot confirm the change will not affect existing services, do not offer the Cloudflare option.

If changing the forwarding would affect other studio machines, the reply card must also explain which machines are affected.

**Completion condition**

- The console port has been obtained.
- Whether forwarding currently exists, and its state, has been confirmed.
- Whether other studio machines are affected has been confirmed.
- The user has confirmed through a reply card the forwarding method to use this time.
- When using cloudflared, the domain to use has been obtained.

## 3. Set up and verify the forwarding

Proceed according to the forwarding method confirmed in step 2.

If the user chose to keep the current forwarding, do not recreate or modify it; confirm its run-at-boot setting, then verify the forwarding URL directly.

If new forwarding needs to be set up:

1. Install the necessary forwarding tool.
2. Complete the necessary account logins.
3. Create forwarding that points to the console's local port.
4. Set it to run automatically after boot.
5. Open the forwarding URL from the console computer, and confirm the response is the console login page, not an error page or another service.

For operations that need administrator privileges or an account login, open an action reply card per the rules above, and continue after the user performs them.

When setting up new forwarding, existing forwarding must stay running. You must not disable existing forwarding to create the new setup.

### cloudflared

If using cloudflared:

- On macOS, you must not use `sudo` when installing the boot service; otherwise it is installed at the wrong level and cannot read the user's configuration file.
- On Linux, when installing the service with `sudo`, you must specify the full path of the configuration file with `--config`.
- Do not take the service being in a running state as proof of success; you must actually confirm through the forwarding URL that the console login page opens normally.

### Tailscale

On macOS the `tailscale` command is not on PATH; when the Tailscale app is not open, the command keeps waiting — it neither exits nor shows an error.

### Tailscale Funnel

If using Tailscale Funnel, opening the forwarding URL from the console computer does not confirm it is reachable from the public internet.

The AI does not verify this itself; in step 4, use an action reply card to ask the user to open the forwarding URL over a mobile network on a phone not connected to Tailscale, and report the result.

### Verification failure

If verification fails, troubleshoot and fix based on the actual error.

If it cannot be resolved, keep the existing working forwarding — do not disable it yourself — and open a reply card explaining the current state and the error for the user to decide how to proceed.

**Completion condition**

- The forwarding checks the AI can perform are done, and the forwarding URL is confirmed to return the console login page.
- The forwarding is set to run automatically after boot.
- The final forwarding URL has been recorded.
- Connection results that need confirmation from an external device are left for the user's acceptance in step 4.

## 4. User acceptance

Open an action reply card asking the user to verify remote access from the device they will actually use. The card provides everything needed for acceptance at once:

- **Forwarding URL**: provide the final forwarding URL and ask the user to open it and log in. If two-factor authentication is enabled, also confirm that two-factor authentication completes normally.
- **How to connect**: provide the steps for the forwarding method used this time:
  - Tailscale (devices in the account only): install the Tailscale app on the phone, log in to the same account as the console computer, then open the forwarding URL.
  - Tailscale Funnel: turn off Tailscale on the phone and open the forwarding URL over a mobile network.
  - cloudflared: open the forwarding URL directly.
- **Optional settings**: tell the user these do not affect this acceptance and can be set up as needed:
  - Passkey: log in with Face ID or a fingerprint later. A passkey only works for the URL it was added on, so add it through the current forwarding URL.
  - Add to Home Screen: add the console to the phone's home screen to open it like a standalone app.
  - Push notifications: receive notifications for new messages or reply cards. On iPhone, the console must first be added to the home screen.
- **Limitations**: remind the user that once the console computer shuts down or sleeps, other devices cannot connect; with cloudflared or Tailscale Funnel, the login page and callback endpoints are exposed to the internet.

Ask the user to reply with the result after logging in. Optional settings are not required and do not affect whether this step is complete.

After the user replies, confirm they have logged in through the final forwarding URL. If they report being unable to connect or log in, troubleshoot based on the actual result; do not treat the reply card reply itself as successful acceptance.

If other studio machines need to switch to this URL, after acceptance succeeds, open a separate action reply card for the machine migration. The card must:

- List the machines that need to switch to the new URL.
- Ask the user to log in through the new URL, go to "Monitor › Machines", and choose "Reinstall" for each machine that needs updating.
- Ask the user to run the resulting install command on the corresponding machine.
- State explicitly that "Add machine / onboard" must not be used, to avoid creating duplicate machine records.

After the user replies that it is done, read back the machine status, confirm all machines needing migration are back online, then restart the members on them.

**Completion condition**

- The user has logged in through the final forwarding URL.
- Any other studio machines that needed to switch to this URL are back online through the new URL.

## 5. Complete the switchover

If the original forwarding was kept this time, this step is not needed.

If a new forwarding method or URL was adopted, after step 4, confirm all studio machines needing migration are back online through the new URL, then deal with the old forwarding.

Before disabling the old forwarding, confirm:

- The new URL still opens the console normally.
- The user can log in through the new URL.
- All affected studio machines are online through the new URL.

Until all of the above conditions hold, you must not disable the old forwarding.

When disabling the old forwarding, remove only the forwarding this change has made unnecessary; you must not also clear other settings still in use.

If using Tailscale, note that Funnel and a private `serve` configuration may coexist. `tailscale funnel ... off` also clears the private forwarding; to disable only Funnel and keep the private forwarding, re-run `tailscale serve --bg <console port>` instead.

After finishing, confirm:

- The old URL can no longer connect. When switching from Funnel to devices in the account only, the URL stays the same; instead confirm that Funnel is off and the private forwarding is still in place.
- The new URL can still log in normally.
- Other studio machines remain online.

If the URL has changed, remind the user:

- Passkeys created on the original URL must be added again on the new URL.
- If external services such as Slack or GitHub use the original URL as a callback endpoint, they must be changed to the new URL.
- Home-screen shortcuts and push notifications on the phone that are tied to the old URL must be set up again with the new URL.

**Completion condition**

- The new URL can still log in normally.
- All affected studio machines remain online.
- The old forwarding has been disabled without affecting other forwarding or services still in use.
