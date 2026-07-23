package prompt

// GopherArt is the ASCII art banner displayed when GopherMind starts: a
// bespectacled, buck-toothed gopher. Kept under ~46 columns so it fits a
// standard terminal without wrapping. Uses single quotes (not backticks) so the
// whole thing can live in a Go raw string literal.
const GopherArt = `
          __                          __
        _/  \________________________/  \_
       /                                   \
      |    .------.            .------.     |
      |   /   __   \          /   __   \    |
      |  |  /(o )\  |________|  /( o)\  |    |
      |  |  \ '' /  |  .--.  |  \ '' /  |    |
      |   \  '--'  / (    )  \  '--'  /     |
      |    '------'   \ __ /   '------'     |
      |                |==|                 |
       \               '--'                /
        \._                             _./
         \ '""--..____________..--"'   /
          '-.._____________________..-'

              G O P H E R M I N D
`

// GoPherItBanner is the "GO PHER IT" tagline wordmark shown directly under
// GopherArt at startup. Kept to 42 columns and indented 8 spaces so it lines
// up centered under the ~46 column gopher and survives an 80-column
// terminal. GopherMind remains the product name; this is tagline art only.
const GoPherItBanner = `
         __  __    _  | |  __  _     _ ___
        / _|/  \  |_) |_| |_  |_)    |  |
        \__|\__/  |   | | |__ | \    |  |
`
