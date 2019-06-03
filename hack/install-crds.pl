use strict;
use warnings;
use Getopt::Long;

my $clean = '';
my $examples = '';

GetOptions(
    "clean" => \$clean,
    "examples" => \$examples
);

for my $crd (sort glob "crd/*.yaml") {
    if ($clean) {
        system("kubectl delete -f $crd");
    }
    system("kubectl", "create", "-f", $crd);
}

exit 0 if !$examples;

for my $example (sort glob "crd/examples/*.yaml") {
    system("kubectl delete -f $example");
    system("kubectl", "create", "-f", $example);
}
