# -*- mode: ruby -*-
# vi: set ft=ruby :

# Vagrantfile API/syntax version. Don't touch unless you know what you're doing!
VAGRANTFILE_API_VERSION = "2"

Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|

  config.vm.box = "precise64"

  config.vm.provider :virtualbox do |vb|
    vb.customize ["modifyvm", :id, "--natdnshostresolver1", "on"]
    vb.customize ["modifyvm", :id, "--natdnsproxy1", "on"]
  end

  # Install docker and pull ubuntu:12:04 image
  config.vm.provision "docker", images: ["ubuntu:12.04", "coreos/etcd"]
  config.vm.provision "docker" do |d|
    d.run "coreos/etcd",
      args: "--name etcd -i -t -p 4001:4001 coreos/etcd"

  end

  # Install deps
  config.vm.provision "shell",
    inline: "apt-get update && apt-get install -yq git-core haproxy"

  # Install go 1.2
  config.vm.provision "shell",
    inline: "curl -s https://go.googlecode.com/files/go1.2.linux-amd64.tar.gz | sudo tar -v -C /usr/local -xz"
  config.vm.provision "shell",
    inline: "grep \"PATH=/usr/local/go/bin:\\$PATH\" .bashrc || echo \"export PATH=/usr/local/go/bin:\\$PATH\" >> .bashrc"

  # Install etcd 0.3
  config.vm.provision "shell",
    inline: "curl -sL https://github.com/coreos/etcd/releases/download/v0.3.0/etcd-v0.3.0-linux-amd64.tar.gz | sudo tar -v -C /usr/local -xz"
  config.vm.provision "shell",
    inline: "grep \"PATH=/usr/local/etcd-v0.3.0-linux-amd64:\\$PATH\" .bashrc || echo \"export PATH=/usr/local/etcd-v0.3.0-linux-amd64:\\$PATH\" >> .bashrc"

  # Setup galaxy go path
  config.vm.provision "shell",
    inline: "mkdir -p go/src/github.com/litl"
  config.vm.provision "shell",
    inline: "ln -sf go/src/github.com/litl /vagrant"
  config.vm.provision "shell",
    inline: "grep \"GOPATH=/home/vagrant/go\" .bashrc || echo \"export GOPATH=/home/vagrant/go\" >> .bashrc"
  config.vm.provision "shell",
    inline: "grep \"PATH=/home/vagrant/go/bin:\\$PATH\" .bashrc || echo \"export PATH=/home/vagrant/go/bin:\\$PATH\" >> .bashrc"
  config.vm.provision "shell",
    inline: "chown -R vagrant.vagrant go"

end
