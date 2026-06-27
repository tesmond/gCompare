# gCompare

gCompare is a Wails + Svelte desktop file comparison tool with a Go backend.

## Current Scope

- Multiple comparison tabs
- Folder comparisons with sorted, aligned entries
- File comparisons with side-by-side line diff rows
- Folder context actions for file copy
- File line selection and in-memory line copy
- Save and discard for modified file comparisons
- Basic binary and large-file handling

## Development

Install the Wails CLI, then run:

```sh
wails dev
```

The comparison core can be tested without the Wails CLI:

```sh
go test ./internal/compare
```
