#!/bin/bash
# wait_for_vm.sh "HOST" [PORT]

HOST=${1:-"windows11.local"}
PORT=${2:-22}
VM_NAME="Windows11"
UTMCTL="/usr/local/bin/utmctl"

echo "Starting VM: $VM_NAME via utmctl..."
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

echo "Waiting for SSH service on $HOST:$PORT to be responsive..."
while ! nc -z -w 5 "$HOST" "$PORT"; do
  echo "SSH not ready yet, retrying..."
  sleep 5
done

echo "SSH service is up on $HOST:$PORT."
echo "VM $VM_NAME is ready for tests."
