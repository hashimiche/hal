# Terraform VCS Workflow Skill Examples

## Example 1: Enable VCS Automation

User: Wire my TFE workspaces to the local GitLab repos.

A:

    hal terraform status
    hal terraform vcs-workflow enable
    hal terraform vcs-workflow

## Example 2: Check VCS Automation State

User: Is GitLab integration active for TFE?

A:

    hal terraform vcs-workflow

## Example 3: Enable For Twin Instance

User: Set up VCS workspace automation on the twin TFE.

A:

    hal terraform vcs-workflow enable --target twin
    hal terraform vcs-workflow --target twin

## Example 4: Reset Workspace Wiring

User: Redo the workspace-to-GitLab wiring from scratch.

A:

    hal terraform vcs-workflow disable --auto-approve
    hal terraform vcs-workflow update

## Example 5: Enable With Custom Branch

User: Wire TFE workspaces but track the `develop` branch.

A:

    hal terraform vcs-workflow enable --branch develop
