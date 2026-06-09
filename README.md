
<img src="./assets/microNeo-logo.svg" width="400px" align="center"/>

**The only terminal Markdown editor that renders and edits in the same window.**

Every Markdown editor splits your screen — source left, preview right.
Terminal screens aren't wide to begin with. **microNeo renders and edits in the same window.**

<img src="./assets/microneo-demo2.png" width="70%"/>

- Click anywhere to edit the source
- See the result instantly, no split panes
- Works great as `$EDITOR` for `Claude Code`, `Yazi`, etc.

**One-line Install**
```bash
curl -fsSL https://raw.githubusercontent.com/sollawen/microNeo/master/install.sh | sh
```

[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.19+-00ADD8.svg)](https://golang.org/)
[![Single Binary](https://img.shields.io/badge/single%20binary-yes-green.svg)]()

---

## Why microNeo

| | microNeo | Micro / nano | glow / leaf | vim + plugins | GUI Editors |
|--|:---------:|:-------------:|:------------:|:------------:|:-------------:|
| Editable | ✓ | ✓ | ✗  | ✓ | ✓ |
| Markdown Rendering | ✓ | ✗  | ✓ | ✓ | ✓ |
| Same Interface | ✓ | - | - | ✗ (split) | ✗ (split) |
| Low Learning Curve | ✓ | ✓ | ✓ | ✗ | ✓ |

**microNeo = Micro's editing + Glow's rendering, in one window.**

---

## Usage
```bash
# Open any file
microneo README.md
```


## Set as Default Editor
```bash
export EDITOR=microneo
```

Works seamlessly with `Claude Code`, `Yazi`, and other tools that use `$EDITOR`.

## Configuration
microNeo uses `$XDG_CONFIG_HOME/microNeo/` for config (defaults to `~/.config/microNeo/`)
- `colorscheme`: Markdown rendering colors can be customized via your color scheme file. Built-in color schemes (darcula, gruvbox-tc, etc.) include these definitions. Custom color schemes go in `~/.config/microNeo/colorschemes/`. 
- `settings.json`: If you see garbled text on mouse click (common on Linux), set clipboard mode to `terminal` in `~/.config/microNeo/settings.json`:

```json
{
  "clipboard": "terminal"
}
```

**Font**: 
- A Nerd Font or similar powerline-compatible font is recommended. 
- If the status separator `` looks broken, change `status-separator` in `~/.config/microNeo/settings.json` (e.g., to `│`).

## Hotkeys
| Action | Shortcut | Action | Shortcut |
|--------|----------|--------|----------|
| Save | `Ctrl-S` | Undo | `Ctrl-Z` |
| Quit | `Ctrl-Q` | Search | `Ctrl-F` |
| Command Mode | `Ctrl-E` | | |

Press `Ctrl-E` and type `help` for more commands.

---

## Relationship with Micro

microNeo is an independent fork of [Micro](https://github.com/micro-editor/micro). It inherits all of Micro's strengths — zero dependencies, intuitive operation, Lua plugins, mouse support — and adds a Markdown rendering layer on top.

microNeo aims to develop independently — like NeoVim to Vim — to fundamentally improve the Markdown experience in the terminal.

---
Reach me via sollawen@gmail.com

## License

[MIT](./LICENSE)
