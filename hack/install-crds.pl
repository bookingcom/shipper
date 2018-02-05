use strict;
use warnings;
use Data::Dumper qw(Dumper);
use Getopt::Long;

my $clean = '';
my $examples = '';

GetOptions(
    "clean" => \$clean,
    "examples" => \$examples
);

for my $crd (sort glob "crd/*.yaml") {
    if ($clean) {
        system("kubectl delete -f $crd &>/dev/null");
    }
    system("kubectl", "create", "-f", $crd);
}

exit 0 if !$examples;

for my $example (sort glob "crd/examples/*.yaml") {
    system("kubectl delete -f $example &>/dev/null");
    system("kubectl", "create", "-f", $example);
}
