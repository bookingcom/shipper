use strict;
use warnings;
use feature qw(say);

use Data::Dumper qw(Dumper);
use Getopt::Long;
my $dry_run;
my $verbose;

GetOptions(
    "dry-run" => \$dry_run,
    "verbose" => \$verbose,
);

use JSON::PP qw(decode_json encode_json);
use CPAN::Meta::YAML qw(Dump);

my $raw = `kubectl get rel --all-namespaces -o json`;
my $list = decode_json($raw);

my $count = 0;
for my $rel (@{$list->{items}}) {
    next if defined $rel->{spec}->{environment};

    my ($ns, $name) = @{$rel->{metadata}}{qw(namespace name)};

    say "$ns/$name";
    $rel->{spec}->{environment} = $rel->{metadata}->{environment};
    if ($verbose) {
        say Dumper($rel);
    }

    if (!$dry_run) {
        open(my $kubectl, "|-", "kubectl", "replace", "-f", "-");
        $kubectl->print(encode_json($rel));
        close($kubectl);
    }
    $count++;
}

say sprintf("modified %s releases out of %s total", $count, scalar @{$list->{items}});
say "(just kidding, it was a dry run)" if $dry_run;
