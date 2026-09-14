# go.nix — this module's dependencies (FDR 0008); go.mod, gomod2nix.toml and
# the package graph are rendered or derived from it inside nix. Edit through
# the escape hatch (godyn-go) or by hand.
{
  flakeInputs = { };
  go = "1.26";
  module = "code.linenisgreat.com/crap/go-crap/v2";
  replace = { };
  require = {
    "github.com/aymanbagabas/go-osc52/v2" = {
      go = "1.16";
      hash = "sha256-6Bp0jBZ6npvsYcKZGHHIUSVSTAMEyieweAX2YAKDjjg=";
      indirect = true;
      version = "v2.0.1";
    };
    "github.com/charmbracelet/bubbles" = {
      go = "1.24.2";
      hash = "sha256-Vz9QgctlzJqggPwfi48Lbn38ZJXu3Y71byp5uuuzUvU=";
      version = "v1.0.0";
    };
    "github.com/charmbracelet/bubbletea" = {
      go = "1.24.0";
      hash = "sha256-7wr85TLszu1CHNEMv+o4w+r24Z0xdzCgecPv+ZtRX/A=";
      version = "v1.3.10";
    };
    "github.com/charmbracelet/colorprofile" = {
      go = "1.24.2";
      hash = "sha256-d/NjM/ybG+bGRRRMMcjbPCFGFS5noZRMaL05Ix5r/II=";
      indirect = true;
      version = "v0.4.1";
    };
    "github.com/charmbracelet/harmonica" = {
      go = "1.16";
      hash = "sha256-fi5N0IXhSbbYHdSZFngCfpT4kdiEaKedqj8YpnlvX0o=";
      indirect = true;
      version = "v0.2.0";
    };
    "github.com/charmbracelet/lipgloss" = {
      go = "1.18";
      hash = "sha256-RHsRT2EZ1nDOElxAK+6/DC9XAaGVjDTgPvRh3pyCfY4=";
      version = "v1.1.0";
    };
    "github.com/charmbracelet/x/ansi" = {
      go = "1.24.2";
      hash = "sha256-UToZIkqXl9MEppcRgbeBqaaMeAzRkGa0w3lVUs6sxWI=";
      indirect = true;
      version = "v0.11.6";
    };
    "github.com/charmbracelet/x/cellbuf" = {
      go = "1.24.2";
      hash = "sha256-0S60XaWhKZG+TB3Kqe1oMn2Okwdq53nym8XayVSHHiM=";
      indirect = true;
      version = "v0.0.15";
    };
    "github.com/charmbracelet/x/term" = {
      go = "1.24.0";
      hash = "sha256-KF7IU1Luxl/sZP6XjomWB2e3lxSUS4/5AahhapGir/4=";
      indirect = true;
      version = "v0.2.2";
    };
    "github.com/clipperhouse/displaywidth" = {
      go = "1.18";
      hash = "sha256-9CNyTZPSncKQ7Y0my9DR4WYXDjtDHYNL512D691WDAM=";
      indirect = true;
      version = "v0.9.0";
    };
    "github.com/clipperhouse/stringish" = {
      go = "1.18";
      hash = "sha256-Mp8M1CRbwr6dcJ4BD9tXD5I78ZgCFEm0GDxJv0GYReg=";
      indirect = true;
      version = "v0.1.1";
    };
    "github.com/clipperhouse/uax29/v2" = {
      go = "1.18";
      hash = "sha256-Men4JLhiuEtAx8ZSzId5ciRWhud68o3k/B48ppwyxkM=";
      indirect = true;
      version = "v2.5.0";
    };
    "github.com/erikgeiser/coninput" = {
      go = "1.16";
      hash = "sha256-OWSqN1+IoL73rWXWdbbcahZu8n2al90Y3eT5Z0vgHvU=";
      indirect = true;
      version = "v0.0.0-20211004153227-1c3628e74d0f";
    };
    "github.com/lucasb-eyer/go-colorful" = {
      go = "1.12";
      hash = "sha256-6BKrJsfmxie+YFAWzTYVPQfrwjQEXRo+J8LY+50C1BU=";
      indirect = true;
      version = "v1.3.0";
    };
    "github.com/mattn/go-isatty" = {
      go = "1.15";
      hash = "sha256-qhw9hWtU5wnyFyuMbKx+7RB8ckQaFQ8D+8GKPkN3HHQ=";
      indirect = true;
      version = "v0.0.20";
    };
    "github.com/mattn/go-localereader" = {
      hash = "sha256-JlWckeGaWG+bXK8l8WEdZqmSiTwCA8b1qbmBKa/Fj3E=";
      indirect = true;
      version = "v0.0.1";
    };
    "github.com/mattn/go-runewidth" = {
      go = "1.20";
      hash = "sha256-GpnbKplhX410Q/eIdknvWbYZgdav1keN+7wNUeOSMHE=";
      indirect = true;
      version = "v0.0.19";
    };
    "github.com/muesli/ansi" = {
      go = "1.17";
      hash = "sha256-qRKn0Bh2yvP0QxeEMeZe11Vz0BPFIkVcleKsPeybKMs=";
      indirect = true;
      version = "v0.0.0-20230316100256-276c6243b2f6";
    };
    "github.com/muesli/cancelreader" = {
      go = "1.17";
      hash = "sha256-uEPpzwRJBJsQWBw6M71FDfgJuR7n55d/7IV8MO+rpwQ=";
      indirect = true;
      version = "v0.2.2";
    };
    "github.com/muesli/termenv" = {
      go = "1.17";
      hash = "sha256-hGo275DJlyLtcifSLpWnk8jardOksdeX9lH4lBeE3gI=";
      indirect = true;
      version = "v0.16.0";
    };
    "github.com/rivo/uniseg" = {
      go = "1.18";
      hash = "sha256-rDcdNYH6ZD8KouyyiZCUEy8JrjOQoAkxHBhugrfHjFo=";
      indirect = true;
      version = "v0.4.7";
    };
    "github.com/xo/terminfo" = {
      go = "1.19";
      hash = "sha256-GyCDxxMQhXA3Pi/TsWXpA8cX5akEoZV7CFx4RO3rARU=";
      indirect = true;
      version = "v0.0.0-20220910002029-abceb7e1c41e";
    };
    "golang.org/x/sys" = {
      go = "1.24.0";
      hash = "sha256-1+i5EaG3JwH3KMtefzJLG5R6jbOeJM4GK3/LHBVnSy0=";
      indirect = true;
      version = "v0.38.0";
    };
    "golang.org/x/text" = {
      go = "1.24.0";
      hash = "sha256-wGKd1JkeiFROibvo2kkAuQ7JajSIfV4utGaoGbTQhQM=";
      indirect = true;
      version = "v0.34.0";
    };
  };
}
