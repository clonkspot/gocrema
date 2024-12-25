{ pkgs ? (
    let
      inherit (builtins) fetchTree fromJSON readFile;
      inherit ((fromJSON (readFile ./flake.lock)).nodes) nixpkgs gomod2nix;
    in
    import (fetchTree nixpkgs.locked) {
      overlays = [
        (import "${fetchTree gomod2nix.locked}/overlay.nix")
      ];
    }
  )
, buildGoApplication ? pkgs.buildGoApplication
, go
}:

buildGoApplication {
  pname = "gocrema";
  version = "0.1";
  pwd = ./.;
  src = ./.;
  modules = ./gomod2nix.toml;
  go = go;

  postPatch = ''
    substituteInPlace crema.go \
      --replace-fail templates/ "$out/templates/"
  '';

  postInstall = ''
    cp -r templates "$out"
  '';
}
