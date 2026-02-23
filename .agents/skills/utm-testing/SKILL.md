---
name: UTM VM Management
description: Instructions and scripts for managing the "Windows11" UTM VM to test Windows-specific Zenith functionality.
---

# UTM VM Management Skill

This skill allows Antigravity to automate testing on a Windows 11 virtual machine running in UTM.

## Commands

Always use the absolute path `/usr/local/bin/utmctl` on the host (macOS).

### 1. VM Lifecycle
- **Start VM**: `/usr/local/bin/utmctl start "Windows11"`
- **Stop VM**: `/usr/local/bin/utmctl stop "Windows11"`
- **Check Status**: `/usr/local/bin/utmctl status "Windows11"`

### 2. Execution & Interaction
- **Run Command**: `/usr/local/bin/utmctl exec "Windows11" -- cmd /c [command]`
- **Wait for IP**: `/usr/local/bin/utmctl ip-address "Windows11"` (Useful to verify the guest agent is responsive).

### 3. File Transfer
- **Push File**: `/usr/local/bin/utmctl file push "Windows11" [host_path] [guest_path]`
- **Pull File**: `/usr/local/bin/utmctl file pull "Windows11" [guest_path] [host_path]`

## Common Workflows

### Waiting for VM Boot
If the VM was just started, you may need to poll for status until it's "running" AND the guest agent is responsive. Use the `scripts/wait_for_vm.sh` script for this.

### Running Zenith Tests
1. Build the Windows binaries: `make build-windows`
2. Push them to the VM:
   ```bash
   /usr/local/bin/utmctl file push "Windows11" bin/win64/zenith-server.exe C:\Users\Public\zenith-server.exe
   /usr/local/bin/utmctl file push "Windows11" bin/win64/zenith-cli.exe C:\Users\Public\zenith-cli.exe
   ```
3. Run the server and test commands via `utmctl exec`.
