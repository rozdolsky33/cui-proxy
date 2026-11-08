# Offline menu-bar manual

The user requested a menu-bar command to open documentation locally, followed by a PR, merge, and release bump. This authorizes the full implementation and release sequence. Release target: v0.4.0 (attachment inspection plus offline documentation).

Add Open Documentation beside Open Dashboard, available in every proxy state. Open a bundled index.html with the default browser through a file URL. Never start a proxy/server or fall back to GitHub. A missing bundle or browser-open failure produces an actionable local alert directing the user to reinstall the menu-bar app. Headless --check reports the documentation URL for diagnostics/testing.

The installer unpacks a generated offline MkDocs snapshot from the Go binary into Contents/Resources/Manual before signing and replacing the app. Installation remains atomic; an unpacking failure leaves the prior app intact. A standalone binary (including the shell installer and Homebrew) therefore carries the manual without relying on a source checkout or sibling files.

Use the existing documentation source and theme with relative .html links, local assets, offline search, and recipe downloads. Disable repository/edit widgets and all automatic network requests; external reference hyperlinks remain intentional. Diagrams must have an offline representation. Check generated inputs for staleness in Go tests and regenerate the deterministic compressed archive when docs change. Keep the ordinary go build workflow free of Python/Node requirements for consumers.

Approaches: bundling a static HTML snapshot is selected because it works with the service stopped and needs no listener. Serving from the dashboard was rejected because it requires a running proxy. Opening raw Markdown was rejected because users need browser-readable navigation and downloads. This is a small extension of the existing menu and docs build, with packaging verification rather than a new application.
