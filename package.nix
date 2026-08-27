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

  vendorHash = "sha256-4ILdOd1f/Spyaj6T3PkmfL3TwMtZOG1OttVzvLORhho=";
}
