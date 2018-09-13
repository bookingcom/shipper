[![Build Status](https://travis-ci.org/bookingcom/shipper.svg?branch=master)](https://travis-ci.org/bookingcom/shipper)

# Shipper: Kubernetes native multi-cluster canary or blue-green rollouts

Shipper is a set of controllers for orchestrating rollouts to one or many
Kubernetes clusters. It uses Helm charts and CRDs to specify what should be
deployed, and how it should be rolled out. It provides functionality on top of
core Kubernetes Deployment objects by automating blue/green or canary rollout
patterns, including traffic shifting between versions.

## What does it do?

Here's a video demo of Shipper from the SIG Apps community call last June. Our
object definitions have changed a little bit since then, but this is still
a good way to get a general idea of what problem Shipper is solving and how it
looks in action.

[![shipper sig-apps demo](https://img.youtube.com/vi/5BLD0d_VzNU/0.jpg)](https://youtu.be/5BLD0d_VzNU?t=96)

## WIP

Shipper is in active use inside Booking.com, but we have a ways to go before it is
a consumable open source product. If you're reading this note on HEAD, it means
we're still working on our first round of docs, installation guides,
communication channels, and so on. Please bear with us while we make the
transition from internal to external :)
