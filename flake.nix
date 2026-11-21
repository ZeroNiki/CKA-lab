{
  description = "CKA Lab: local CKA trainer (Go server + kubeadm stand on Vagrant: libvirt or VirtualBox)";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      system = "x86_64-linux";
      # vagrant is BSL-licensed (unfree in nixpkgs); allow only it.
      pkgs = import nixpkgs {
        inherit system;
        config.allowUnfreePredicate = pkg: builtins.elem (nixpkgs.lib.getName pkg) [ "vagrant" ];
      };
    in
    {
      devShells.${system}.default = pkgs.mkShell {
        packages = with pkgs; [
          go
          gopls
          shellcheck
          kubectl
          vagrant
          curl
        ];
        # vagrant-libvirt ships with vagrant from nixpkgs; VirtualBox is not installed here.
        VAGRANT_DEFAULT_PROVIDER = "libvirt";
      };
    };
}
