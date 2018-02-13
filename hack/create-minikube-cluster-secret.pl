use strict;
use warnings;

use Data::Dumper qw(Dumper);
use File::Temp;
use MIME::Base64;
use Getopt::Long;

# convenience script to create a secret and cluster based on minikube certs

use constant {
    SECRET_TEMPLATE => 0,
    CLUSTER_TEMPLATE => 1
};

my $home = $ENV{"HOME"};
my $host = `minikube ip`;

my $cluster_name = "minikube";
my $namespace = "default";
my $client_key_file = "$home/.minikube/client.key";
my $client_cert_file = "$home/.minikube/client.crt";
my $ca_cert_file = "$home/.minikube/ca.crt";

my $clean = '';

GetOptions(
    "clean" => \$clean,
    "host=s" => \$host,
    "name=s" => \$cluster_name,
    "namespace=s" => \$namespace,
    "key=s" => \$client_key_file,
    "cert=s" => \$client_cert_file,
    "ca=s" => \$ca_cert_file
);
chomp($host);

my %secret_files = (
    key => $client_key_file,
    cert => $client_cert_file,
    ca => $ca_cert_file,
);

my @templates = split "---", do { local $/; <DATA> };

my %secret;
for my $item (sort keys %secret_files) {
    my $file = $secret_files{$item};
    open my $fh, "<", $file
        or die "can't open $item $file: $!";

    my $contents = do { local $/; <$fh> };
    chomp($contents);
    # second arg is "line terminating char". we want the whole file contents as
    # a single line, so ''.
    $secret{"tls.$item"} = encode_base64($contents, '');
    chomp($secret{"tls.$item"});
}

my $secret_fh = File::Temp->new(UNLINK => 1);
printf $secret_fh
    $templates[SECRET_TEMPLATE],
    $secret{"tls.ca"},
    $secret{"tls.cert"},
    $secret{"tls.key"},
    $cluster_name,
    $namespace;

my $cluster_fh = File::Temp->new(UNLINK => 1);
printf $cluster_fh
    $templates[CLUSTER_TEMPLATE],
    $cluster_name,
    $host;

if ($clean) {
    system("kubectl", "delete", "-f", $secret_fh->filename);
    system("kubectl", "delete", "-f", $cluster_fh->filename);
}

system("kubectl", "create", "-f", $secret_fh->filename);
system("kubectl", "create", "-f", $cluster_fh->filename);

__DATA__

apiVersion: v1
data:
  tls.ca: %s
  tls.cert: %s
  tls.key: %s
kind: Secret
metadata:
  name: %s
  namespace: %s
type: Opaque
---
apiVersion: shipper.booking.com/v1
kind: Cluster
metadata:
  name: %s
spec:
  apiMaster: https://%s:8443
  capabilities: []
  region: local
