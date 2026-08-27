#!/usr/bin/env bash

set -Eeuo pipefail

artifact_dir=${1:?artifact directory is required}
cli=${2:?CLI is required}
kubeconfig=${3:?kubeconfig is required}
context=${4:?context is required}
qualification_uid=${5:?qualification UID is required}

[ "$(id -u)" -eq 0 ]
[ -d "${artifact_dir}" ]
[ -x "${cli}" ]
[ -f "${kubeconfig}" ]
[[ "${qualification_uid}" =~ ^[0-9]+$ ]]

ssh_dir=/tmp/qualification-ssh
mkdir -p "${ssh_dir}" /run/sshd
ssh-keygen -q -t ed25519 -N '' -f "${ssh_dir}/host"
ssh-keygen -q -t ed25519 -N '' -f "${ssh_dir}/client"
install -d -m 700 -o "${qualification_uid}" /tmp/qualification-home/.ssh
install -m 600 -o "${qualification_uid}" "${ssh_dir}/client.pub" /tmp/qualification-home/.ssh/authorized_keys
passwd -d qualification >/dev/null

cat > "${ssh_dir}/sshd_config" <<EOF
Port 2222
ListenAddress 127.0.0.1
HostKey ${ssh_dir}/host
PidFile ${ssh_dir}/sshd.pid
AuthorizedKeysFile .ssh/authorized_keys
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
AllowUsers qualification
UsePAM no
PrintMotd no
Subsystem sftp internal-sftp
EOF
/usr/sbin/sshd -D -f "${ssh_dir}/sshd_config" -E "${ssh_dir}/sshd.log" &
sshd_pid=$!
cleanup() {
  kill "${sshd_pid}" 2>/dev/null || true
  wait "${sshd_pid}" 2>/dev/null || true
  rm -rf -- "${ssh_dir}" /tmp/qualification-home/.ssh
}
trap cleanup EXIT

deadline=$((SECONDS + 10))
while ! ssh-keyscan -p 2222 127.0.0.1 > "${ssh_dir}/known_hosts" 2>/dev/null; do
  [ "${SECONDS}" -lt "${deadline}" ] || exit 1
  sleep 0.2
done
chown "${qualification_uid}" "${ssh_dir}/client" "${ssh_dir}/known_hosts"
chmod 600 "${ssh_dir}/client" "${ssh_dir}/known_hosts"
runuser -u qualification -- ssh -p 2222 \
  -i "${ssh_dir}/client" \
  -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes \
  -o "UserKnownHostsFile=${ssh_dir}/known_hosts" \
  qualification@127.0.0.1 printf ssh-ready | grep -Fxq ssh-ready

runuser -u qualification -- python3 /scripts/pty_check.py \
  --cli "${cli}" --kubeconfig "${kubeconfig}" --context "${context}" \
  --collector-namespace kube-memlens \
  --profile tmux-linux --terminal-version 3.4 --term xterm-256color \
  --columns 120 --rows 30 --duration-seconds 30 \
  --exit-mode normal --colour-mode native --transport tmux \
  --output "${artifact_dir}/tmux-120x30.json"

tc qdisc add dev lo root netem delay 80ms 10ms
runuser -u qualification -- python3 /scripts/pty_check.py \
  --cli "${cli}" --kubeconfig "${kubeconfig}" --context "${context}" \
  --collector-namespace kube-memlens \
  --profile ssh-linux --terminal-version openssh-9.6p1 --term xterm-256color \
  --columns 120 --rows 30 --duration-seconds 30 \
  --exit-mode normal --colour-mode native --transport ssh \
  --ssh-port 2222 --ssh-user qualification \
  --ssh-identity "${ssh_dir}/client" --ssh-known-hosts "${ssh_dir}/known_hosts" \
  --output "${artifact_dir}/ssh-latency-120x30.json"
tc qdisc del dev lo root
