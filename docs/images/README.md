# Documentation images

Screenshots referenced by the README and the documentation pages live here. They are
produced from the admin panel, either against a real instance or against the mock API
(`npm run mock` in `web/`), so that no real hostnames, bucket names or tokens appear.

| File | What it shows |
|---|---|
| `dashboard.png` | The dashboard with stat tiles, the 30 day chart, storage per destination and recent runs |
| `job-detail.png` | A job page with its overview cards, run history and artifacts |
| `run-log.png` | A run detail page with the stage timeline and the live log viewer, dark theme |
| `restore.png` | The restore dialog on an artifact, showing the path and source modes |
| `job-editor.png` | The job editor with the schedule builder, the next occurrences and the packing options |
| `destinations.png` | The destinations list with usage bars |
| `dashboard-dark.png` | The same dashboard in the dark theme |
| `hosts.png` | The hosts list with a tested SSH host, the tools found on it and its source count |
| `host-dialog.png` | The host dialog with a generated key pair, the public key and the `authorized_keys` snippet |

Guidelines: capture at 1440 by 900 in the light theme, except the run log and the dark
dashboard which are captured in the dark theme, set the browser timezone to UTC so the times
in the panel match the documented convention, export as PNG, and keep each file under 400 KB.
