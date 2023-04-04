#!/bin/bash


ceph_conf="~/.ceph/ceph.config"
keyring="~/.ceph/ceph.keyring"
pool="eci"


ls_pool



ls_pool() {
    echo "------eci pool-----"
    rdb -c $ceph_conf -k $keyring ls $pool
}

create_disk() {
    rbd -c $ceph_conf -k $keyring
}
