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

  vendorHash = "sha256-KNFebwp1PCZ8wVhntqUIya+HimwgIDmuRg984yamG6U=";
}
