{
  buildGoModule,
  rev
}: let
  src = ./.;
  version = builtins.readFile "${src}/VERSION";
in buildGoModule {
  pname = "btrfs-nfs-csi";
  inherit version;
  inherit src;

  ldflags = [
    "-X main.version=${version} -X main.commit=${rev}"
  ];

  subPackages = [ "cmd/btrfs-nfs-csi" ];

  vendorHash = "sha256-gsXMgLNf5XxFFF8YzcWzCM2K9mbsccyOUu8bwYR5iu8=";
}
