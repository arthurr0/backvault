# Palette

The exact colour tokens Backvault uses, with the contrast measurements that decide where
each one may be used. The token names are part of the contract between the brand assets and the
admin panel, use them verbatim.

## Tokens

```css
:root {
  --ink: #0F1720;
  --ink-2: #182230;
  --brass: #D4A032;
  --brass-2: #F2C14E;
  --parchment: #F6F3EC;
  --paper: #FFFFFF;
  --slate-1: #334155;
  --slate-2: #64748B;
  --slate-3: #94A3B8;
  --line: #E5E1D8;
  --line-dark: #263241;
  --success: #2E9E6B;
  --warning: #E0A100;
  --danger: #D9483B;
  --info: #3B82F6;
  --running: #7C3AED;
}
```

| Token | Hex | What it is for |
|---|---|---|
| `--ink` | `#0F1720` | Dark theme page background, light theme body text, the vault body |
| `--ink-2` | `#182230` | Dark theme cards, table headers, hovered rows |
| `--brass` | `#D4A032` | Primary buttons, the vault handle and hinges, active navigation, focus rings |
| `--brass-2` | `#F2C14E` | Hover state of a brass control, accents on dark surfaces, the handle on the dark mark |
| `--parchment` | `#F6F3EC` | Light theme page background, the vault door inset |
| `--paper` | `#FFFFFF` | Light theme cards, modals, inputs |
| `--slate-1` | `#334155` | Secondary text on light surfaces |
| `--slate-2` | `#64748B` | Muted text, labels, table meta on light surfaces |
| `--slate-3` | `#94A3B8` | Placeholder text, disabled labels, secondary text on dark surfaces |
| `--line` | `#E5E1D8` | Borders and dividers on light surfaces |
| `--line-dark` | `#263241` | Borders and dividers on dark surfaces |
| `--success` | `#2E9E6B` | Successful runs, present artifacts |
| `--warning` | `#E0A100` | Partial success, overdue jobs, verify mismatches |
| `--danger` | `#D9483B` | Failed runs, destructive actions |
| `--info` | `#3B82F6` | Neutral information, queued runs |
| `--running` | `#7C3AED` | A run in progress |

## Contrast

Measured as WCAG 2.1 contrast ratios. The threshold for body text is 4.5, for large text
and for non text UI shapes it is 3.0.

| Foreground on background | Ratio | Verdict |
|---|---|---|
| `--ink` on `--parchment` | 16.28 | Body text, light theme |
| `--ink` on `--paper` | 18.05 | Body text on cards |
| `--parchment` on `--ink` | 16.28 | Body text, dark theme |
| `--slate-1` on `--paper` | 10.35 | Secondary text |
| `--slate-2` on `--paper` | 4.76 | Muted text, still passes |
| `--slate-3` on `--paper` | 2.56 | Placeholders and disabled text only |
| `--slate-3` on `--ink` | 7.04 | Secondary text on dark |
| `--ink` on `--brass` | 7.63 | The only safe way to put text on a brass button |
| `--brass` on `--ink` | 7.63 | Accent text and icons on dark |
| `--brass-2` on `--ink` | 10.75 | Hover and highlight on dark |
| `--brass` on `--paper` | 2.37 | Never for text, borders and fills only |
| `--success` on `--paper` | 3.38 | Icons, pills and large text, not body text |
| `--warning` on `--paper` | 2.27 | Shapes only, pair it with a dark label |
| `--danger` on `--paper` | 4.25 | Large text and shapes, add weight for small text |
| `--running` on `--ink` | 3.17 | Shapes only on dark, use `--info` for text |

Two rules follow from the table and they are not negotiable:

- Brass never carries text on a light background. A primary button is brass with `--ink`
  text, never brass text on paper.
- Status colours are carriers of shape and fill. A status pill uses a tinted background
  with a dark or light label, not coloured text on white.

## Status pills

| Status | Surface | Label |
|---|---|---|
| success | `--success` at 14 percent opacity | `--success` on light, `#7EDCB0` on dark |
| warning | `--warning` at 16 percent opacity | `#8A6400` on light, `--warning` on dark |
| danger | `--danger` at 14 percent opacity | `#A6281C` on light, `#F09A91` on dark |
| info | `--info` at 12 percent opacity | `#1D4ED8` on light, `#93B8FB` on dark |
| running | `--running` at 12 percent opacity | `#5B21B6` on light, `#C4B5FD` on dark |

## Focus and selection

Focus rings are `--brass` at 2 px with a 2 px offset in both themes. On a brass control the
ring switches to `--ink` on light and `--parchment` on dark so it stays visible. Text
selection is `--brass` at 25 percent opacity.

## Typography

Inter for the interface, JetBrains Mono for logs, paths, checksums and anything the operator
may need to compare character by character. Both always ship with a fallback stack.

```css
--font-ui: Inter, "Inter var", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto,
           Helvetica, Arial, sans-serif;
--font-mono: "JetBrains Mono", ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas,
             "Liberation Mono", monospace;
```

The wordmark is Inter SemiBold with letter spacing -0.01em. In the SVG files that value is
written as an absolute length for the font size in use, because some renderers ignore em
units in the presentation attribute.
