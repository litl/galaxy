# -*- mode: ruby -*-
# vi: set ft=ruby :

# Vagrantfile API/syntax version. Don't touch unless you know what you're doing!
VAGRANTFILE_API_VERSION = "2"

$script = <<SCRIPT
  # redis-cli
  apt-get update
  apt-get install -y curl
  curl -s http://download.redis.io/releases/redis-2.8.8.tar.gz | tar -v -C /tmp -xz && pushd /tmp/redis-2.8.8 && make && sudo make install
  popd
  rm -rf /tmp/redis-2.8.8

  # go 1.2
  curl -s https://go.googlecode.com/files/go1.2.linux-amd64.tar.gz | sudo tar -v -C /usr/local -xz
  grep \"PATH=/usr/local/go/bin:\\$PATH\" .bashrc || echo \"export PATH=/usr/local/go/bin:\\$PATH\" >> .bashrc

  # Setup galaxy go path
  mkdir -p /home/vagrant/go/src/github.com/litl
  ln -sf /vagrant /home/vagrant/go/src/github.com/litl/galaxy
  grep \"GOPATH=/home/vagrant/go\" .bashrc || echo \"export GOPATH=/home/vagrant/go\" >> .bashrc
  grep \"PATH=/home/vagrant/go/bin:\\$PATH\" .bashrc || echo \"export PATH=/home/vagrant/go/bin:\\$PATH\" >> .bashrc
  chown -R vagrant.vagrant go
  export PATH=/usr/local/go/bin:$PATH
  export GOPATH=/home/vagrant/go
  go get github.com/tools/godep
  go get github.com/mattn/goreman
SCRIPT


Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|

  config.vm.box = "woven-docker"

  config.vm.provider :virtualbox do |vb|
    vb.customize ["modifyvm", :id, "--natdnshostresolver1", "on"]
    vb.customize ["modifyvm", :id, "--natdnsproxy1", "on"]
  end

  # Install docker and pull ubuntu:12:04 image
  config.vm.provision "docker", images: ["ubuntu:12.04", "orchardup/redis"]
  config.vm.provision "docker" do |d|
    d.run "orchardup/redis",
      args: "--name redis -d -p 6379:6379"
  end

  config.vm.provision "shell", inline: $script
end
