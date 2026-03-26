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

  vendorHash = "sha256-tW4yH0L+KbV/4LRC2+obTNUiQBMomMAj7k86bU3MkxY=";
}
