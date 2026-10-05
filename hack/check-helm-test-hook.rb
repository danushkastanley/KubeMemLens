#!/usr/bin/env ruby

require 'yaml'

documents = YAML.load_stream(File.read(ARGV.fetch(0))).compact
hooks = documents.select do |document|
  document['kind'] == 'Job' &&
    document.dig('metadata', 'labels', 'app.kubernetes.io/name') == 'kube-memlens-test'
end
abort 'expected exactly one connection test hook' unless hooks.length == 1

job = hooks.first.fetch('spec')
abort 'connection hook must have one bounded Job attempt' unless
  job.fetch('backoffLimit') == 0 && job.fetch('activeDeadlineSeconds') == 300
template = job.fetch('template')
abort 'connection hook Pod must match its network policy' unless
  template.dig('metadata', 'labels', 'app.kubernetes.io/name') == 'kube-memlens-test'
spec = template.fetch('spec')
containers = spec.fetch('containers')
abort 'expected exactly one connection test container' unless containers.length == 1
container = containers.fetch(0)
image = container.fetch('image')
abort 'connection hook image must be digest-pinned' unless
  image.match?(/\A[a-z0-9][a-z0-9.\/:_-]*@sha256:[a-f0-9]{64}\z/)
expected_resources = {
  'requests' => { 'cpu' => '1m', 'memory' => '4Mi' },
  'limits' => { 'memory' => '16Mi' }
}
abort 'connection hook lacks its tested startup memory allowance' unless
  container.fetch('resources') == expected_resources

security = container.fetch('securityContext')
abort 'connection hook container security settings changed' unless
  security == {
    'privileged' => false,
    'readOnlyRootFilesystem' => true,
    'allowPrivilegeEscalation' => false,
    'capabilities' => { 'drop' => ['ALL'] }
  }
abort 'connection hook Pod security settings changed' unless
  spec.fetch('securityContext') == {
    'runAsNonRoot' => true,
    'runAsUser' => 65_532,
    'runAsGroup' => 65_532,
    'seccompProfile' => { 'type' => 'RuntimeDefault' }
  }
abort 'connection hook must not mount a ServiceAccount token' unless
  spec.fetch('automountServiceAccountToken') == false
abort 'connection hook must not restart to hide startup failure' unless
  spec.fetch('restartPolicy') == 'Never'

abort 'unsupported connection hook check option' unless ARGV.length == 1 ||
  (ARGV.length == 2 && ARGV[1] == '--image')
puts ARGV[1] == '--image' ? image : 'connection hook startup resources and security settings passed'
