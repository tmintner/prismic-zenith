#!/bin/bash
# wait_for_vm.sh "VM_NAME"

VM_NAME=$1
UTMCTL="/usr/local/bin/utmctl"

echo "Starting VM: $VM_NAME..."
$UTMCTL start "$VM_NAME" 2>/dev/null || echo "VM might already be running."

echo "Waiting for VM $VM_NAME to report 'running' status..."
while true; do
  STATUS=$($UTMCTL status "$VM_NAME" | grep -v "UUID" | awk '{print $2}')
  if [ "$STATUS" == "running" ]; then
    echo "VM is running."
    break
  fi
  sleep 2
done

echo "Waiting for guest agent to be responsive (checking IP)..."
while true; do
  IP=$($UTMCTL ip-address "$VM_NAME" 2>/dev/null)
  if [ ! -empty "$IP" ] && [ "$IP" != "No IP addresses found." ]; then
    echo "Guest agent is ready. IP: $IP"
    break
  fi
  sleep 5
done

echo "VM $VM_NAME is ready for tests."
