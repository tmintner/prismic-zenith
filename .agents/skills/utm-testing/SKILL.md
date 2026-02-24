---
name: UTM VM Management (SSH)
description: Instructions and scripts for managing the "Windows11" UTM VM via SSH to test Windows-specific Zenith functionality.
---

# UTM VM Management Skill (SSH)

This skill allows Antigravity to automate testing on a Windows 11 virtual machine using `utmctl` for lifecycle and SSH for remote interaction.

## Prerequisites
- **OpenSSH Server** must be enabled on the Windows VM.
- The VM should be reachable via a hostname (default: `windows11.local`) or a static IP.
- SSH keys should be configured to avoid password prompts.

## Commands

### 1. VM Lifecycle (Host Side)
- **Start VM**: `/usr/local/bin/utmctl start "Windows11"`
- **Stop VM**: `/usr/local/bin/utmctl stop "Windows11"`
- **Check Status**: `/usr/local/bin/utmctl status "Windows11"`

### 2. Execution & Interaction (Remote)
- **Run Command**: `ssh user@windows11.local "cmd /c [command]"`
- **Example**: `ssh user@windows11.local "whoami"`

### 3. File Transfer
- **Push File**: `scp [host_path] user@windows11.local:[guest_path]`
- **Example**: `scp bin/win64/zenith-server.exe user@windows11.local:C:/Users/Public/`

## Common Workflows

### Waiting for SSH Readiness
If the VM was just started, use the `scripts/wait_for_vm.sh` script to poll port 22 until the SSH service is responsive.

### Running Zenith Tests
1. Build the Windows binaries: `make build-windows`
2. Push them to the VM:
   ```bash
   scp bin/win64/zenith-server.exe user@windows11.local:C:/Users/Public/
   scp bin/win64/zenith-cli.exe user@windows11.local:C:/Users/Public/
   ```
3. Run the server and test commands via `ssh`.
