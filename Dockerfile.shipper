FROM alpine:3.8
LABEL authors="Parham Doustdar <parham.doustdar@booking.com>, Alexey Surikov <alexey.surikov@booking.com>, Igor Sutton <igor.sutton@booking.com>, Ben Tyler <benjamin.tyler@booking.com>"
ADD shipper /bin/shipper
ENV PATH=/bin
ENTRYPOINT ["shipper", "-disable", "clustersecret", "-v", "4", "-logtostderr"]
