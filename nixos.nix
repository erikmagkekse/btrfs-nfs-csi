{
  config,
  lib,
  pkgs,
  rev,
  ...
}:
let
  cfg = config.services.btrfs-nfs-csi;

  inherit (lib)
    mkOption
    mkIf
    mapAttrs'
    nameValuePair
    attrValues
    ;
  inherit (lib.types) package;

  agentOpts = { name, ... }: {
    options = {
      basePath = mkOption {
        type = lib.types.str;
        default = "/export/data";
        description = "Base path for btrfs subvolume creation (AGENT_BASE_PATH)";
      };
      environmentFile = mkOption {
        type = lib.types.nullOr lib.types.path;
        default = null;
        description = "Path to environment file containing secrets such as AGENT_TENANTS";
      };
      listenAddr = mkOption {
        type = lib.types.str;
        default = ":8000";
        description = "Address for the agent to listen on (AGENT_LISTEN_ADDR)";
      };
      metricsAddr = mkOption {
        type = lib.types.str;
        default = "127.0.0.1:9090";
        description = "Metrics server address (AGENT_METRICS_ADDR)";
      };
    };
  };
in
{
  options.services.btrfs-nfs-csi = {
    package = mkOption {
      type = package;
      description = "Specify btrfs-nfs-csi package";
    };
    agent = mkOption {
      type = lib.types.attrsOf (lib.types.submodule agentOpts);
      default = { };
      description = "BTRFS-NFS CSI agent instances";
    };
  };

  config = {
    assertions = lib.flatten (attrValues (mapAttrs' (name: options:
      nameValuePair "btrfs-nfs-csi-agent-${name}" {
        assertion = options.environmentFile != null;
        message = "services.btrfs-nfs-csi.agent.${name}.environmentFile must be set because AGENT_TENANTS is required to run the service";
      }
    ) cfg.agent));

    # This parodies deploy/agent/agent.service
    systemd.services = mapAttrs' (name: options:
      nameValuePair "btrfs-nfs-csi-agent-${name}" {
        description = "btrfs-nfs-csi agent (${name})";
        after = [ "network.target" ];
        environment = {
          AGENT_BASE_PATH   = options.basePath;
          AGENT_LISTEN_ADDR = options.listenAddr;
          AGENT_METRICS_ADDR = options.metricsAddr;
        };
        path = [
          pkgs.btrfs-progs
          pkgs.nfs-utils
        ];
        script = "${cfg.package}/bin/btrfs-nfs-csi agent";
        serviceConfig = {
          EnvironmentFile = options.environmentFile;
          Restart = "always";
          RestartSec = "5";
        };
        wantedBy = [ "multi-user.target" ];
      }
    ) cfg.agent;
  };
}

