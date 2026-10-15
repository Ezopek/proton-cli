# proton pass folders

Folders that sort the items inside a vault.

Every command under `proton pass folders`, with the arguments and flags it takes. For these commands in use, see [the pass guide](README.md).

Holds `create`, `delete`, `list` and `update`.

## `create`

Create a folder, at the top of a vault or inside another folder.

Making folders needs a paid Pass plan. --parent names the folder by its path from its vault, Personal/Banking, or by its ID.

```
proton pass folders create
```

```bash
proton pass folders create --name Banking --vault Personal
proton pass folders create --name Cards --parent Personal/Banking
```

| Flag | Description |
| --- | --- |
| `--name string` | Name for the new folder |
| `--parent string` | Put it inside this folder, by path or ID |
| `--vault string` | Which vault, by name or ID (default: your first) |

## `delete`

Delete folders, and everything inside them.

The items and folders inside a folder are deleted with it, and nothing brings them back.

```
proton pass folders delete REF...
```

```bash
proton pass folders delete Personal/Old
```

## `list`

List the folders in your vaults.

A hidden vault's folders are left out unless --vault names it.

```
proton pass folders list
```

```bash
proton pass folders list
proton pass folders list --vault Personal
```

| Flag | Description |
| --- | --- |
| `--vault string` | Match only this vault, by name or ID |

## `update`

Rename a folder.

Renaming needs a paid Pass plan.

```
proton pass folders update REF
```

```bash
proton pass folders update Personal/Banking --name Finance
```

| Flag | Description |
| --- | --- |
| `--name string` | The folder's new name |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
