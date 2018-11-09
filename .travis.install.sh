#!/bin/sh

set -ex

wget http://apt-stable.ntop.org/`lsb_release -r | cut -f2`/all/apt-ntop-stable.deb
sudo dpkg -i apt-ntop-stable.deb
sudo apt-get update
sudo apt-get install linux-headers-generic pfring-dkms pfring qemu-system-x86 libpcap-dev -y
