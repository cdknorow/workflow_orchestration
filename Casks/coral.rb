cask "coral" do
  version "2.3.1"
  sha256 ""  # TODO: fill after DMG is published

  url "https://github.com/cdknorow/coral/releases/download/v#{version}/Coral.dmg"
  name "Coral"
  desc "Multi-agent orchestration system for AI coding agents"
  homepage "https://github.com/cdknorow/coral"

  depends_on formula: "tmux"

  app "Coral.app"

  zap trash: [
    "~/.coral",
  ]

  caveats <<~EOS
    Coral requires tmux for agent management.
    tmux has been installed as a dependency.

    Launch Coral from your Applications folder or Spotlight.
    The dashboard runs at http://localhost:8420.
  EOS
end
