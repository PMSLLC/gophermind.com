# Brief: clipboard history

A small macOS menu bar utility that keeps a running history of everything
copied to the clipboard (text only is fine for a first version), shown
as a dropdown from a status bar icon. Clicking an item in the history
copies it back to the clipboard.

Should keep a reasonable number of recent items (a few dozen), persist
the history across app restarts, and let the user clear it. Launching at
login would be nice but isn't required for a first version.

Not a target for this brief: image/file clipboard support, syncing
across devices, or a full preferences window -- keep it to the one
thing it needs to do well.
