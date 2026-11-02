---
name: new
description: Create a new Go app from the ReadySetGo template (github.com/ukharwa/ReadySetGo) and set it up for the app the user describes, with the database, UI mode and modules it needs.
argument-hint: "[describe the app, e.g. a Postgres API with teams]"
disable-model-invocation: true
---

# New ReadySetGo project

The user wants a new project from the ReadySetGo template. What they said
about it, if anything: $ARGUMENTS

This skill only gets a copy of the template onto disk. Everything after
that (checking the machine, choosing the options, correcting requests the
template doesn't support, running `make init`) is described in the copy's
own `AGENTS.md`, which is always current; follow that, not your memory of
the template.

## 1. Where you are

- If the current folder is itself an uninitialised ReadySetGo checkout
  (`tools/readysetgo/` exists), skip to step 4 and use this folder.
- If it's a project that init already created (its `AGENTS.md` has a
  "This project" section and no `tools/readysetgo/`), say init has already
  run there and stop: a new project goes in its own folder.
- Otherwise the project goes in a new folder. ReadySetGo is a whole app,
  never something added to an existing project, so don't create it inside
  another git repository: default to a sibling of the current folder.

## 2. Ask, in one message

Use what the user already said (in the arguments and the conversation) and
ask only for what's missing, recommending an answer for each:

- the project's name: lowercase letters, digits and hyphens. It names the
  folder and the GitHub repository, and becomes init's `NAME`;
- where to create the folder (propose the sibling folder's full path);
- whether to create a GitHub repository (recommended), and if so the
  owner (their account, `gh api user -q .login`, or an organisation) and
  whether it's private (recommended) or public.

You can ask about the app itself here too (database, UI, modules), but
the decisions are made in step 4. Create nothing until they answer.

## 3. Make the copy

Check the folder doesn't exist yet (or is empty) before you start.

**With a GitHub repository** (needs `gh auth status` to pass): from the
parent folder,

```bash
gh repo create OWNER/NAME --template ukharwa/ReadySetGo --private --clone
```

(`--public` instead if they chose that). This is GitHub's "Use this
template": a repository of their own whose history starts with one
commit. GitHub fills it in a few seconds after creating it, so if the
clone comes back empty, wait and run `gh repo clone OWNER/NAME` again.

**Without one:**

```bash
git clone https://github.com/ukharwa/ReadySetGo.git NAME
git -C NAME remote remove origin
```

The project then keeps the template's history and has no remote, which is
fine; they can add one later. Don't make a shallow clone: GitHub refuses
to push one into a new repository.

## 4. Follow the copy's AGENTS.md

Read `NAME/AGENTS.md` in full and follow "Starting a new project" from
step 1, with the new folder as the working directory for every command
(`git -C`, `make -C`, or `cd NAME && …`). Carry over what you already
know: don't ask again for the name, or for anything the user described.
The `MODULE` follows from the repository: `github.com/OWNER/NAME`.

Its rules hold here too: propose the `make init` command and wait for a
yes, and ask before committing.

## 5. Hand over

When AGENTS.md's last step is done, tell the user to open their coding
agent in the new folder for the work that follows: that folder's
`AGENTS.md` (and `CLAUDE.md`) now describe their project, and a session
started there reads them.
