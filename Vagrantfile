# -*- mode: ruby -*-
# CKA Lab stand: kubeadm cluster on vagrant-libvirt.
require "fileutils"
require "securerandom"

# Change the node set here only.
NODES = [
  { name: "controlplane", ip: "192.168.56.10", cpus: 2, memory: 4096, role: "controlplane" },
  { name: "node01",       ip: "192.168.56.11", cpus: 2, memory: 2048, role: "worker" },
  { name: "node02",       ip: "192.168.56.12", cpus: 2, memory: 2048, role: "worker" },
].freeze

CONTROLPLANE_IP = NODES.find { |n| n[:role] == "controlplane" }[:ip]
LAB_DIR = File.join(__dir__, ".lab")
TOKEN_FILE = File.join(LAB_DIR, "token")

# A fixed bootstrap token lets workers join without passing the join command back
# from the guest: the rsync synced folder only goes host -> guest.
unless File.exist?(TOKEN_FILE)
  FileUtils.mkdir_p(LAB_DIR)
  alphabet = [*"a".."z", *"0".."9"]
  token = "#{Array.new(6) { alphabet[SecureRandom.random_number(alphabet.size)] }.join}." \
          "#{Array.new(16) { alphabet[SecureRandom.random_number(alphabet.size)] }.join}"
  File.write(TOKEN_FILE, token)
  File.chmod(0o600, TOKEN_FILE)
end
BOOTSTRAP_TOKEN = File.read(TOKEN_FILE).strip

Vagrant.configure("2") do |config|
  config.vm.box = "generic/ubuntu2204"
  config.vm.synced_folder ".", "/vagrant", type: "rsync",
    rsync__exclude: [".git/", ".lab/", ".superpowers/", ".vagrant/"]

  NODES.each do |node|
    config.vm.define node[:name] do |vm|
      vm.vm.hostname = node[:name]
      vm.vm.network "private_network", ip: node[:ip], libvirt__network_name: "cka-lab"

      vm.vm.provider :libvirt do |lv|
        lv.cpus = node[:cpus]
        lv.memory = node[:memory]
      end

      vm.vm.provision "common", type: "shell", path: "scripts/common.sh", args: [node[:ip]]
      if node[:role] == "controlplane"
        vm.vm.provision "controlplane", type: "shell", path: "scripts/controlplane.sh",
          args: [BOOTSTRAP_TOKEN, CONTROLPLANE_IP]
        # Addons need all three nodes Ready, which is only true after every VM is up,
        # so `./lab up` runs this explicitly: vagrant provision controlplane --provision-with addons
        vm.vm.provision "addons", type: "shell", path: "scripts/addons.sh", privileged: false,
          run: "never"
      else
        vm.vm.provision "worker", type: "shell", path: "scripts/worker.sh",
          args: [BOOTSTRAP_TOKEN, CONTROLPLANE_IP]
      end
    end
  end
end
