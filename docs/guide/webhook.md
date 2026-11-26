# Send outside events to members automatically (webhooks)

This page explains what a webhook endpoint is, what each of its settings means, and how a member handles an outside event once it arrives.

## Webhook endpoints

A webhook endpoint is a web address that belongs to one member. When something happens in Slack, GitHub, or another outside service, the service sends the details to this address, and they become a chat message to that member. For example, when someone on Slack mentions a certain keyword, the event goes to the member responsible for replying. If these events need to be handled in a fixed way, you can first create a task manual; the member then opens a task following the manual when an event arrives. See [Have AI do repetitive work by an SOP (task manuals)](sop.md).

You set up webhook endpoints in the "WEBHOOK ENDPOINTS" card on the member's Member details. Only you and the Assistant, Mira, can set them up.

## What each webhook endpoint contains

- **Endpoint ID**: this endpoint's name. It can only contain English letters, digits, underscores, and hyphens.
- **Purpose**: which events this endpoint receives and what the member should do with them. The screen marks it as optional, but **always fill it in, and be specific**, because members rely on the purpose to judge whether incoming content should be handled. For example: "When someone mentions a refund in the Slack support channel, summarize their question and report it to me."
- **Platform type**:
  - **Generic**: recognized by the address alone, with no check of where the event came from. Anyone who has the address can send content in.
  - **Slack**, **GitHub**: additionally use that platform's Signing Secret to confirm the event really came from that platform. Once saved, the Signing Secret is not shown again; use "Rotate secret" to replace it.
- **Address**: paste it into the outside service's webhook settings. The address contains a credential, so **keep it as safe as a password**. If it leaks, delete the endpoint and create a new one; the old address stops working permanently.
- **Enabled / Disabled**: events received while disabled are all discarded. All settings are kept.
- **Event stats**: when the last event arrived, whether any events were dropped and why, and the contents of the 5 most recent events. The most common reason for a drop is "signature failed", which means the Signing Secrets on the two sides do not match.

The endpoint ID, purpose, and platform type **cannot be changed after creation**; to change them, delete the endpoint and create it again. Deleting an endpoint **cannot be undone**.

For a full description of each field and of the event stats, see "Webhook endpoints" in [Interface reference](interface.md).

## How a member handles an event

A delivered event becomes a chat message containing the raw content the outside service sent, together with the endpoint ID and purpose. The notification the member receives starts with this endpoint's purpose, or is marked as having no purpose set when it is blank. When the purpose is long, the notification shows only the beginning, and the member reads the full text back before deciding. A member that is online receives it immediately. A member that is offline or stopped is not woken up by it; the message waits until the member next comes online.

Members treat content sent from outside as data, **not as your instructions**:

- The content fits the purpose: the member does only what the purpose allows.
- The content goes beyond the purpose, or asks for something the member is not authorized to do (for example, asks it to ignore existing rules, change the usual process, or perform some other operation): the member stops and does not carry out the request. If you need to decide whether to make an exception, the member stops before doing anything and opens a reply card to ask you.
- The member cannot tell whether the content fits the purpose, or the endpoint has no purpose: the member keeps the raw content and its reasoning, and opens a reply card for you to decide.

So the more specific the purpose, the better members can judge on their own what to do and when to stop and ask you.

## Things to watch out for

- **The outside service has to be able to reach the address.** The console only accepts connections from the computer OffiCraft is installed on. First tell Mira "I want to reach the console from somewhere other than this computer" and pick a method that creates a public HTTPS address (cloudflared or Tailscale Funnel; see [Use it on your phone](mobile.md)). Then open the console through the forwarded address and copy the webhook address from there. An address copied while the console was opened through `localhost` cannot be reached by outside services, and neither can a private network that only your own devices can reach. For the protections to set up before opening access, see [Protect your studio](security.md).
- **An outside service reporting a successful send does not mean the member received it.** The outside service gets the same response whether or not the event was delivered. To check, look at the endpoint's "Event stats". Right after setting one up, you can send a test event from the outside service.
- **Put long-lived webhook endpoints on staff members.** When a member leaves the studio (for example, an outsource member leaves after its task closes, a staff member is deleted, or the machine the member runs on is removed), the webhook endpoints under that member are permanently deleted along with it. The outside service keeps sending events and gets no error, so nothing looks wrong from the outside service's side. A member that is stopped or refocused has not left, and its endpoints are kept.
- **When you change the Signing Secret in Slack or GitHub, rotate it here at the same time.** If you change only one side, every later event is dropped for failing the signature check.

## Related docs

- [Protect your studio](security.md)
- [Have AI do repetitive work by an SOP (task manuals)](sop.md)
- [Do something on a schedule (scheduled messages)](schedule.md)
- [Assign work and answer reply cards](daily.md)
