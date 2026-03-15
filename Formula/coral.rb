class Coral < Formula
  include Language::Python::Virtualenv

  desc "Multi-agent orchestration system for AI coding agents with a web dashboard"
  homepage "https://github.com/cdknorow/coral"
  url "https://files.pythonhosted.org/packages/source/a/agent-coral/agent_coral-2.2.0.tar.gz"
  sha256 ""  # TODO: fill after PyPI publish
  license "MIT"

  depends_on "python@3.12"
  depends_on "tmux"

  def install
    virtualenv_install_with_resources
  end

  def caveats
    <<~EOS
      Coral is installed! To get started:

        # Start the web dashboard
        coral

        # Launch agents in worktrees
        launch-coral /path/to/worktrees

        # macOS menu bar app (optional)
        pip install rumps && coral-tray

      Dashboard runs at http://localhost:8420 by default.
    EOS
  end

  test do
    assert_match "Coral Dashboard", shell_output("#{bin}/coral --help")
  end
end
