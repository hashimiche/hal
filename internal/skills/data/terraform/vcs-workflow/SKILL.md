---
name: vcs-workflow
description: Configure a Terraform VCS-driven workspace automation lab with local GitLab reuse in hal. Use for `hal terraform vcs-workflow`, `hal tf vcs`, `hal tf ws`, or `hal tf workspace` commands.
---

# Terraform VCS Workflow Skill

This skill handles the VCS-driven workspace automation lifecycle through `hal terraform vcs-workflow`.

## Intent

Use this skill when the user asks to:

- wire TFE workspaces to local GitLab VCS triggers
- bootstrap the VCS-driven demo lab (repos, OAuth, workspaces)
- disable or reset workspace automation
- check whether workspace-to-GitLab integration is active
- run VCS-triggered Terraform plans or applies via the lab
- configure workspace settings (branch, tags regex, project, org)

## Core Commands

- `hal terraform vcs-workflow` — smart status view (default when no subcommand given)
- `hal terraform vcs-workflow enable` — bootstrap GitLab VCS integration and workspace wiring
- `hal terraform vcs-workflow disable` — remove workspace-to-VCS bindings
- `hal terraform vcs-workflow update` — re-run enable over existing state

Short aliases: `hal tf vcs enable`, `hal tf ws enable`, `hal tf workspace enable`

## Target Support

All subcommands accept `--target` to scope to primary or twin TFE instance:

- `hal terraform vcs-workflow enable` — primary TFE (default)
- `hal terraform vcs-workflow enable --target twin` — twin TFE instance
- `hal terraform vcs-workflow enable --target both` — both TFE instances

## Lab Assumptions

- TFE local endpoint: `https://tfe.localhost:8443`
- GitLab local endpoint: `http://gitlab.localhost:8929`
- GitLab container: `hal-gitlab`
- Default GitLab password: `hal9000FTW` (override with `--gitlab-password`)
- Default TFE org: `hal` (override with `--tfe-org`)
- Default workspace branch: `main`
- Prerequisite: TFE must be running before enabling VCS workflow

## What The Command Actually Sets Up

- Registers a GitLab OAuth application in TFE
- Imports or creates scenario GitLab repos under the configured group
- Creates TFE workspaces wired to the corresponding GitLab repos
- Configures VCS triggers (branch filter or tags regex) per workspace
- Seeds demo working directories under `/workspaces` inside the helper container

## Key Flags

- `--gitlab-password` — GitLab root password (default `hal9000FTW`)
- `--tfe-org` — TFE organization name (default `hal`)
- `--tfe-project` — TFE project to assign workspaces to
- `--tfe-workspace` — restrict operation to a single workspace
- `--tags-regex` — VCS trigger tags regex pattern
- `--branch` — VCS branch to track (default `main`)
- `--auto-approve` — skip confirmation prompts

## Validation

- Confirm GitLab container is running before enable.
- Confirm TFE runtime is running before enable.
- Confirm workspace status with `hal terraform vcs-workflow`.
- After enable, verify a VCS-triggered run can be queued from GitLab.
