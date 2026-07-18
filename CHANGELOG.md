Unreleased
  + interactive picker hides installed skills in every category, including Core

0.3.5
  + expanded the embedded catalog with the Core category and additional skills

0.3.4
  + interactive picker now keeps the Core category visible and selectable
    when its skills are already installed
  + update workflows to use the correct github repo

0.3.1
  + -a opencode flag added to npx skills add command (project-level needs agent)
  + bumped default agent to opencode for project installations

0.3.0
  + changelog subcommand: prints this log via 'skills-pick changelog'
  + --version flag: prints the current version and exits
  + update subcommand: runs 'npx --yes skills update' to refresh all installed skills
  + interactive mode now hides skills that are already installed in the current
    project or globally (~/.agents, ~/.claude, ~/.config/opencode and the
    project's .agents, .claude, .opencode skill directories)
  + verified npx 'skills add' takes only one source per invocation, so the
    installer continues to group by source and run one npx per source

0.2.0
  + add <source> <skill...> [-c category] [-d description]: register a skill
    in the local catalog (~/config/skills-pick/catalog.json)
  + interactive TUI: j/k to move, space to toggle, / to filter, enter to
    install, q to quit
  + direct install mode: pass skill names as positional args to install
    without entering the TUI
  + loadCatalog() prefers --catalog flag, then the user config file, then
    the embedded catalog baked into the binary

0.1.0
  + initial release: interactive picker, 'list' subcommand, direct install
  + embedded catalog (catalog.json) ships a curated set of skills across
    Engineering, Design, SEO & Marketing, and Tools categories
  + runInstall() groups selected skills by source and invokes
    'npx --yes skills add <source> -y --skill <name>' per source
