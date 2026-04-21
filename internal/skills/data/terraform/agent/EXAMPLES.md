# Terraform Agent Skill Examples

## Example 1: Enable Agent Runtime

User: Enable TFE agent pool support for this local stack.

Assistant:

    hal terraform status
    hal terraform agent enable
    hal terraform agent

## Example 2: Recover Stopped Agent

User: UI says selected pool has no registered agents.

Assistant:

    hal terraform agent
    hal terraform agent update
    hal terraform agent

## Example 3: Disable Agent Runtime

User: Tear down local custom agent runtime.

Assistant:

    hal terraform agent disable
    hal terraform agent
