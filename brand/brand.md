# Brand

The name, the voice and the marks, and the rules for using them.

## The name

A vault is the room you put things in when losing them is not an option: one door, one
handle, one place where everything is accounted for. Backvault is that room for your
backups. The name says what the product holds and what it does with it, and it needs no
explanation in a sales page or a terminal prompt.

Always written **Backvault**, capital B, one word, never BACKVAULT, never Back Vault and
never backvault in prose. The binary, the CLI, the Go module and the config keys are
lowercase `backvault`.

## Tagline

Primary: **Every backup, accounted for.**

Secondary, for places where the promise matters more than the bookkeeping: **Backups you
can actually restore.**

The primary tagline is about accountability: the product does not only take backups, it
tells you which ones exist, which ones are missing and which ones are too old. Use the
secondary one on pages about restore and verification.

## Voice

Calm, precise, operator to operator. The reader is on call at 03:00 and needs a fact, not
enthusiasm.

- State what happened, then what to do about it.
- No exclamation marks anywhere in the interface or the documentation.
- No superlatives, no "blazing fast", no "simply", no "just run".
- Prefer the concrete noun: "the run failed in the upload stage", not "something went wrong".
- Error messages name the thing that failed and the next step.
- English everywhere, including logs and code identifiers.

Good: `Upload to storage-box failed after 3 attempts: connection refused. The artifact is
still in the spool and the run is marked failed.`

Not good: `Oops! Something went wrong while uploading your backup.`

## The mark

A vault door drawn flat on a 24 unit grid. A rounded square body in `--ink`, a circular door
inset in `--parchment`, a four spoked handle in `--brass` at the centre of the door, and two
small brass hinge tabs on the left edge of the body. Nothing rotates, nothing is shaded:
flat fills only, no gradients, no shadows, no filters.

The geometry is fixed and should be copied, not redrawn. Body `x=1 y=1 w=22 h=22 rx=5.2`,
door centred at `13.2, 12` with radius `7`, spokes `9.4 x 1.8` crossed at 45 degrees, hub
radius `2.5` with a `0.9` opening cut back to the door colour, hinge tabs `3 x 5` at `x=2.5`.
The door sits slightly right of centre so the hinge side stays open; that offset is the one
asymmetry in the mark and it is what stops the handle from reading as a close button.

| File | Use |
|---|---|
| `logo.svg` | Mark and wordmark, for light backgrounds |
| `logo-dark.svg` | Mark and wordmark, for dark backgrounds |
| `logo-mark.svg` | Mark only, light backgrounds, readable down to 16 px |
| `logo-mark-dark.svg` | Mark only, dark backgrounds |
| `favicon.svg` | Mark bled to the edges of an `--ink` tile, for browser tabs and app icons |
| `social-card.svg` | 1200 x 630 card on `--ink`: mark, wordmark, tagline |

On dark backgrounds the body becomes `--parchment`, the door is cut in `--ink` and the
handle becomes `--brass-2`, which carries 10.75 contrast against the ink door. The hinge
tabs stay `--brass` in both variants: the deeper brass is the only one that still holds its
shape against parchment, and keeping them brass is what makes the two variants read as the
same object seen in two lights.

`favicon.svg` is a separate drawing, not `logo-mark.svg` on a tile. The body fills the whole
24 unit square, the door grows to radius `7.6` and the hinge tabs grow with it, so the mark
survives a 16 px tab on a light or a dark browser chrome.

## Using the marks

- Clear space around the logo is at least a quarter of the mark's height on every side, and
  nothing else may enter it: no text, no rules, no other logos.
- Minimum size: 20 px tall for the mark with the wordmark, 16 px for the mark alone. Below
  20 px drop the wordmark rather than shrinking it.
- Use `favicon.svg` wherever the icon sits on an unknown background or inside a rounded app
  tile. Use `logo-mark.svg` and `logo-mark-dark.svg` when the background is known.
- Do not recolour the mark outside the palette, do not add gradients, shadows or outlines, do
  not rotate it, do not stretch it, do not separate the handle from the door, and do not
  place the light variant on a dark photograph.
- The wordmark in `logo.svg`, `logo-dark.svg` and `social-card.svg` is live text in Inter
  SemiBold with the fallback stack from `palette.md`, so it renders everywhere and stays
  editable. `letter-spacing` is written as an absolute value equal to -0.01em at that font
  size (-0.34 at 34 px, -0.88 at 88 px) because some renderers ignore em units in the
  presentation attribute. Convert the text to outlines before sending a file to a printer or
  to anyone who does not have Inter installed.
- The SVGs are hand written and carry no comments, no embedded fonts and no external
  references. Keep them that way when you edit them.

## Screenshots

Panel screenshots use the light theme by default with the `--parchment` background, and the
dark theme when the subject is the log viewer. Never show real hostnames, real bucket names
or real tokens: use `db-prod`, `web-files`, `storage-box`, `s3-backups`, and truncate every
token to a short prefix such as `bvt_9f2c...`.

## Colour

See `palette.md` for the tokens, the contrast measurements and the rules that follow from
them.

## Coffer to Backvault

The product shipped its early work under the name **Coffer**, with a strongbox mark: a body
with a brass lid resting slightly open and a keyhole. It was renamed to **Backvault** on
**2026-09-17**, and the mark was redrawn from the strongbox to the vault door at the same
time. The old logo files are gone, the palette did not change and neither did the tagline.

What this means in practice:

- The CLI binary is `backvault`. There is no `coffer` binary and no alias.
- The Go module, the config keys, the service name and the container image use `backvault`.
- Anything still saying Coffer, coffer or strongbox is stale. Fix it rather than reproducing
  it, and do not describe the rename as a rebrand or a relaunch: it is a name correction and
  the product is continuous across it.
