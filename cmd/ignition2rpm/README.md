# ignition2rpm

# ! Do not depend on this or use this for anything, it is a science experiment !

Tooling to convert ignition to RPM files for use in rpm-ostree images

This really just uses rpmpack ( github.com/google/rpmpack ) to package CoreOS's ignition format into an RPM file


Process a local ignition into an rpm: 
```
ignition2rpm --config machine_config.ign --output ./machine_config.rpm 
```

Process a remote ignition into an rpm:
```
ignition2rpm --config http://host.example.com/machine_config.ign --output ./machine_config.rpm 
```

Also works with MachineConfig ( or at least it seems to, but I haven't tested this exhaustively ). 

Things to be aware of: 
	- It relocates some files from /usr/local to /var/usrlocal to make rpm-ostree happy 
	- I haven't tested the code for links or directories (since right now the MCO doesn't support those either) 
