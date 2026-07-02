# How to Edit a Video via Slack

A plain-English guide for anyone who wants to get a video edited by typing
requests into Slack -- no software to install, nothing to build, no terminal.

If you can send a Slack message, you can use this.

---

## How it works, in one paragraph

There's one shared computer in the office (the "hub") with Adobe Premiere
Pro and an AI assistant connected to it. That assistant is listening in a
dedicated Slack channel. You type what you want in plain English -- "trim
the intro down to 10 seconds," "add our logo in the corner" -- and it makes
the edit directly inside the real Premiere Pro project on the hub machine.
When it's done, it replies to tell you what it did, and the finished file
shows up wherever exports are set up to land (usually a shared Google Drive
folder).

---

## 1. Find the channel

Ask whoever set this up which Slack channel to use (something like
`#premiere-edits`). Make sure you've been added to it. Everyone in that
channel is talking to the same assistant, working on the same Premiere Pro
project at the same time -- so it behaves like a shared editing bay, not a
private assistant.

## 2. Start a request

Just type what you want, as a normal message in the channel:

```
Cut a 30-second promo from clip_04.mp4 and add a lower-third with the
speaker's name
```

The bot reacts with a 👀 to show it saw your message, then works on it. When
it's done, it replies **in a thread under your message** with a short
summary of what it did.

That thread is now "your" conversation for that request. Everything from
here on, you should keep inside it.

## 3. Keep talking in the thread

Reply **inside the thread** (not a new message in the channel) to keep
refining the same edit:

```
you: Cut a 30-second promo from clip_04.mp4
bot: Done -- created a 30s sequence from clip_04.mp4 named "Promo Cut".

you (in thread): Make the intro a bit slower and add some music under it
bot (in thread): Slowed the first 3 seconds to 75% speed and added a music
                 bed from the stock library.

you (in thread): Perfect, now export it as 1080p for YouTube
bot (in thread): Exported "Promo Cut" as 1080p H.264 to the shared export
                 folder.
```

Each thread remembers the conversation that happened in it, so you don't
need to repeat context. A **new top-level message** in the channel (not a
reply) starts a completely separate conversation -- use that when you're
starting a different video project, so the two don't get mixed up.

## 4. Uploading your own footage

You can drag and drop a video, audio, or image file straight into Slack,
either as its own message or attached to a message with instructions:

```
[attached: interview_take3.mov]
Import this and use it to replace the shaky clip at the start
```

The bot downloads the file and imports it into the Premiere Pro project
automatically -- you don't need to put it anywhere on a shared drive first.

## 5. Starting over

If you finished one video and want to move on to something unrelated, say
so in the channel (as a new top-level message, not a thread reply):

```
new project
```

("reset" or "start over" also work.) This clears the assistant's memory of
the previous conversation so it doesn't get confused by leftover context
from the last video.

## 6. Getting your finished video

Once you ask for an export, the bot's reply will tell you what it exported
and confirm it's done. The actual file lands wherever the export folder is
configured -- typically a folder that syncs automatically to Google Drive,
so it shows up for everyone with access, usually within a minute or two of
the export finishing. If you're not sure where that is, ask whoever runs
the hub machine.

---

## What kinds of requests work well

You don't need special syntax -- just describe the outcome. Some examples
across common tasks:

| You want to... | Say something like... |
|---|---|
| Start a rough cut | "Edit this video using script.pdf with the footage in the Footage bin" |
| Trim something | "Cut clip_02 down to just the first 15 seconds" |
| Add a transition | "Add a cross dissolve between the first two clips" |
| Add titles / names | "Add a lower-third that says 'Jane Doe, Marketing Lead' at the start" |
| Fix audio | "The interview audio is too quiet, bring it up to a normal level" |
| Color | "Warm up the color a bit on the outdoor shots" |
| Add music | "Add a music bed under the whole thing and duck it under the dialogue" |
| Captions | "Auto-generate subtitles for this" |
| Multiple versions | "Export this as both a 16:9 for YouTube and a vertical 9:16 for Instagram" |
| Check before sending | "Does this have any silent gaps or black frames I should know about?" |

If a request is ambiguous, the bot will usually ask a follow-up question in
the thread rather than guess -- just answer it like you would a colleague.

---

## Good habits

- **One thread per video.** Reply in-thread for anything related to the
  video you already started; start a fresh message for a different one.
- **Be specific about which clip/section** when there's more than one clip
  in play ("the second clip," "the intro," "clip_04.mp4") -- it helps more
  than a vague "make it better."
- **Ask it to check its own work.** Things like "play back the last 10
  seconds" or "does the audio clip anywhere?" are fair game.
- **It's fine to iterate.** Small follow-up requests in the same thread
  ("a little faster," "move that up 2 seconds") are exactly how this is
  meant to be used.

## Things to know

- **Only one video project at a time.** The hub can only have one Premiere
  Pro project open. If someone else is mid-edit on a different video, wait
  for them to say they're done (or ask them to start over) before beginning
  a new, unrelated project.
- **Don't message from the CLI and Slack at the same time on the same
  project.** If someone's also typing commands directly on the hub machine
  for the same video, your Slack requests can collide with theirs. Check
  with the room first if you're not sure.
- **It only knows what's in the thread.** If you switch to a new
  top-level message or someone runs "reset," it starts with a clean slate
  and won't remember earlier decisions unless you repeat them.

---

## If something goes wrong

- **No 👀 reaction and no reply after a minute or two:** the bot (or the
  hub machine, or Premiere Pro itself) is probably not running. Ping
  whoever administers the hub.
- **The bot replies with an error or "Failed: ...":** read the message --
  it's usually specific (e.g. a file wasn't found, or a clip name didn't
  match anything in the project). Try rephrasing with the exact file or
  clip name, or check that the footage was actually imported.
- **It seems to have forgotten something you said earlier:** you may have
  accidentally started a new thread instead of replying in the existing
  one, or someone ran "reset." Just restate what you need.
- **Nothing seems to be happening in Premiere Pro itself:** that's normal --
  the assistant drives Premiere Pro directly on the hub machine, so you
  won't see anything unless you're standing in front of that computer. Trust
  the Slack reply as the source of truth for what happened.

---

*This guide covers using the Slack bot as an end user. If you're setting up
the bot itself or the underlying software, see
[`docs/USER_MANUAL.md`](USER_MANUAL.md) instead.*
