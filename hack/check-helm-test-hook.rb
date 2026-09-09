#!/usr/bin/env ruby

require 'yaml'

documents = YAML.load_stream(File.read(ARGV.fetch(0))).compact
hooks = documents.select do |document|
  document['kind'] == 'Pod' &&
    document.dig('metadata', 'labels', 'app.kubernetes.io/name') == 'kube-memlens-test'
end
abort 'expected exactly one connection test hook' unless hooks.length == 1

spec = hooks.first.fetch('spec')
container = spec.fetch('containers').fetch(0)
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

puts 'connection hook startup resources and security settings passed'
