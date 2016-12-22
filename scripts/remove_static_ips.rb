#!/usr/bin/env ruby

require 'yaml'

ARGV.each do |arg|
  manifest = YAML.load_file arg
  manifest["jobs"].map do |job|
    job["networks"].map do |network|
      network["static_ips"] = nil
    end

    job
  end

  puts YAML.dump(manifest)
end
