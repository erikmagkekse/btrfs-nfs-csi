{
  description = "Kubernetes Enterprise storage vibes for your homelab.";

  inputs = {
    flake-parts.url = "github:hercules-ci/flake-parts";
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    systems.url = "github:nix-systems/default-linux";
  };

  outputs =
    inputs@{ self, flake-parts, ... }: let
      rev = inputs.self.shortRev or inputs.self.dirtyShortRev;
    in flake-parts.lib.mkFlake { inherit inputs; } {
      systems = import inputs.systems;
      perSystem =
        { pkgs, ... }:
        {
          packages = rec {
            btrfs-nfs-csi = pkgs.callPackage ./package.nix { inherit rev; };
            default = btrfs-nfs-csi;
          };
        };
      flake.nixosModules = rec {
        btrfs-nfs-csi = { pkgs, lib, ... }: {
          imports = [ ./nixos.nix ];
          services.btrfs-nfs-csi.package = lib.mkDefault
            self.packages.${pkgs.stdenv.hostPlatform.system}.btrfs-nfs-csi;
        };
        default = btrfs-nfs-csi;
      };
    };
}

