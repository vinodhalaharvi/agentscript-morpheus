( ask "Plan a 5-day trip to Dubrovnik, Croatia. Include top attractions, best time to visit, and local tips."
  <*> places_search "top attractions Dubrovnik Croatia"
  <*> places_search "best restaurants Dubrovnik old town"
  <*> places_search "hotels near Dubrovnik old town"
)
>=> merge
>=> ask "Create a detailed 5-day itinerary from this information. Include daily schedule with morning, afternoon, and evening activities. Add restaurant recommendations for each day."
>=> maps_trip "Dubrovnik Adventure"
>=> ask "Translate the following key phrases to Croatian for travelers: Hello, Thank you, Where is the bathroom?, How much does this cost?, The bill please, Delicious!, Can you help me?"
>=> translate "Croatian"
>=> doc_create "Dubrovnik Trip Plan"
>=> email "vinod.halaharvi@gmail.com"
