#!/usr/bin/env ruby

require 'yaml'

cf_manifest = YAML.load_file ARGV[0]
cf_api_manifest = YAML.load_file ARGV[1]

consul_servers = cf_manifest["properties"]["consul"]["agent"]["servers"]["lan"] 
cf_api_manifest["properties"]["consul"]["agent"]["servers"]["lan"] = consul_servers

nats_properties = cf_manifest["properties"]["nats"]
cf_api_manifest["properties"]["nats"] = nats_properties

File.open(ARGV[1], 'w+') {|f| f.write YAML.dump(cf_api_manifest) }
