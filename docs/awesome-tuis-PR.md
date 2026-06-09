# PR: Add microNeo to Editors

## PR Title
Add microNeo to Editors

## PR Body
`microNeo` is a fork of Micro that adds in-place Markdown rendering — open a .md file and see it rendered immediately, click anywhere to edit. No split panes.

It's the only terminal editor that renders and edits Markdown in the same window. Similar tools like Glow/frogmouth are viewers (read-only), and other Markdown editors use split panes. microNeo does both in one view.

Built in Go, single binary, one-line install.

https://github.com/sollawen/microNeo

Thanks!

---

## README change (Editors section)

Add this line after the `micro` entry, in alphabetical order within the section:

```
- [microNeo](https://github.com/sollawen/microNeo) A fork of Micro with in-place Markdown rendering — view and edit in the same window
```

### Exact placement

Insert after this existing line:
```
- [micro](https://github.com/zyedidia/micro) A modern and intuitive terminal-based text editor
```

So it becomes:
```
- [micro](https://github.com/zyedidia/micro) A modern and intuitive terminal-based text editor
- [microNeo](https://github.com/sollawen/microNeo) A fork of Micro with in-place Markdown rendering — view and edit in the same window
- [nino](https://github.com/evanlin96069/nino) A small terminal-based text editor written in C.
```

## Steps

1. Fork rothgar/awesome-tuis
2. Edit README.md — add the one line in the Editors section
3. Commit with message: "Add microNeo to Editors"
4. Open PR with the title and body above
