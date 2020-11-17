# sia-bootstrap

This project helps to bootstrap the [Sia daemon](https://sia.tech). A
configuration file is used to specify a desired state - fully synced, wallet
initialized and unlocked - and `sia-bootstrap` will run the necessary commands
to bring `siad` into this state. This allows to run the lengthy bootstrapping
procedure largely unattended.

One use case consists of using `sia-bootstrap` as part of a systemd unit file for
`siad`. This ensures that other services, that depend on `siad`, will only start
after the Sia daemon is fully operational.

## Installation

    $ git clone https://github.com/javgh/sia-bootstrap.git
    $ cd sia-bootstrap
    $ go install            # will - by default - install to ~/go/bin/

## Usage

Create the following configuration file as `~/.config/sia-bootstrap/config`:

    consensus_location = "./consensus/consensus.db"
    consensus_bootstrap = "https://siasetup.info/consensus.zip"
    #consensus_bootstrap = "https://siastats.info/bootstrap/bootstrap.zip"
    ensure_wallet_initialized = true
    wallet_password = "password"
    #wallet_seed = "your seed here"
    ensure_wallet_unlocked = true
    #ensure_recovery = true

The tool is divided into two phases: "pre" and "post".
During the "pre" phase `sia-bootstrap` will check whether the consensus database
already exists. If not, it will download a third-party snapshot of the database
to speed up the initial blockchain download. If you would rather not rely on a
third party for the initial sync, simply comment out or remove all lines
starting with `consensus_` and `sia-bootstrap` will let `siad` download the
blockchain on its own.

The default value for `consensus_location` will work if you start
`sia-bootstrap` in the same directory as `siad`. Otherwise, you should provide
an absolute path to the consensus database. The consensus snapshot configured
with `consensus_bootstrap` needs to be a ZIP archive and contain a file named
`consensus.db`. The two example bootstrap files are courtesy of
[SiaSetup](https://siasetup.info/tools/consensus) and
[SiaStats.info](https://siastats.info/consensus).

If no wallet already exists and `ensure_wallet_initialized = true` is set,
`sia-bootstrap` will initialize one. It will be encrypted with the password
provided by `wallet_password`. A specific seed is used if it is provided
via `wallet_seed`. Otherwise, the wallet will have a fresh seed.

Use `ensure_wallet_unlocked = true` to ensure that the freshly created or
already existing wallet will be unlocked.

Finally, `ensure_recovery = true` can be used if `sia-bootstrap` should trigger
a recovery scan. It will only do so, if `siad` reports no contracts. However,
any backups that might be recovered during the recovery process need to be
restored manually.

Trigger the "pre" phase before starting `siad` and let the "post" phase run
afterwards:

    $ mkdir sia
    $ cd sia
    $ sia-bootstrap pre
    Consensus database not present at /home/jan/sia/consensus/consensus.db
    Downloading snapshot from https://siasetup.info/consensus.zip
    $ siad

In a new terminal:

    $ cd sia
    $ sia-bootstrap post
    Waiting for Sia daemon to become available...
    Waiting for Sia daemon to sync...
    Initializing wallet with fresh seed...
    Unlocking wallet...

## systemd

Template for a systemd unit file. Make sure to modify the user and various paths
to fit your system. Create as `/etc/systemd/system/siad.service` and enable with
`systemctl enable siad`. The service will start on next system boot or manually
by running `systemctl start siad`. Check the status with `systemctl status siad`
or `journalctl -u siad -f`.

````
[Unit]
Description=Sia daemon
After=multi-user.target

[Service]
Type=simple
User=jan
WorkingDirectory=/home/jan/sia
ExecStartPre=/home/jan/go/bin/sia-bootstrap pre
ExecStart=/home/jan/bin/siad
ExecStartPost=/home/jan/go/bin/sia-bootstrap post
Restart=always
RestartSec=30
TimeoutStartSec=infinity
TimeoutStopSec=30min

[Install]
WantedBy=multi-user.target
````
