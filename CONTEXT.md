# HAL

HAL (HashiCorp Academy Labs) builds disposable HashiCorp product labs on a single
user's machine, and lets that user, or an LLM acting for them, inspect and drive them.

## Language

### The lab

**Lab**:
The set of resources HAL has created on the user's machine. It is disposable by
design: destroying it loses nothing that cannot be recreated.
_Avoid_: environment, stack (when meaning the whole lab)

**HAL-owned resource**:
A container, volume, network, cluster or VM that HAL itself created. It is the only
kind of thing a `hal` command may change or delete.
_Avoid_: HAL resource (ambiguous with "anything HAL can see")

**Lab credential**:
A secret HAL generates or hard-codes for a lab, such as a root token or a demo
password. It is disposable with the lab.
_Avoid_: secret (too broad: it also covers the TFE license, which is not a lab
credential)

### Driving HAL from an LLM

**HAL Plus**:
HAL's own chat interface: a conversational lab assistant backed by a local model.
_Avoid_: HAL+, halplus

**Read command**:
A `hal` command that observes the lab without changing it.
_Avoid_: safe command, status tool

**Action**:
Any `hal` command that is not a read command. An action is treated as such until it
is explicitly marked as a read command.
_Avoid_: write, mutation, enable tool

**Host-path flag**:
A `hal` flag whose value designates a file or directory on the user's machine
outside the lab. An LLM may not use one.
_Avoid_: path flag (many path-like flags refer to paths inside the lab)

**Session-ending command**:
A command that, when run from a chat, also tears down the connection that chat is
using.
_Avoid_: self-destructive command
