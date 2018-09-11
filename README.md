[![Build Status](https://travis-ci.org/bookingcom/shipper.svg?branch=master)](https://travis-ci.org/bookingcom/shipper)

# Shipper: Kubernetes native multi-cluster canary or blue-green rollouts

Shipper is a set of controllers for orchestrating rollouts to multiple
Kubernetes clusters. It uses Helm charts and CRDs to specify what should be
deployed, and how it should be rolled out.

## What does it do?

Here's a video demo of Shipper from the SIG Apps community call last June. Our
object definitions have changed a little bit since then, but this is still
a good way to get a general idea of what problem Shipper is solving and how it
looks in action.

<iframe width="640" height="400" src="https://www.youtube.com/embed/5BLD0d_VzNU?start=93" frameborder="0" allow="autoplay; encrypted-media" allowfullscreen></iframe>

## WIP

Shipper is in active use inside Booking.com, but we have a ways to go before it is
a consumable open source product. If you're reading this note on HEAD, it means
we're still working on our first round of docs, installation guides,
communication channels, and so on. Please bear with us while we make the
transition from internal to external :)
